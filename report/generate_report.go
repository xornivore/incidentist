package report

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	filloutPlaceholder = "  _TODO: please fill out_"
)

type GenerateReportRequest struct {
	Team       string
	PdTeam     string
	Since      string
	Until      string
	TagFilters []string
	AuthToken  string
	Urgency    string
	Replace    []string
	DdApiKey   string
	DdAppKey   string
}

func GenerateReport(request GenerateReportRequest) (string, error) {
	sinceAt, untilAt, err := parseDates(request.Since, request.Until)
	if err != nil {
		return "", err
	}

	incidents, err := fetchIncidents(request.Team, request.DdApiKey, request.DdAppKey, sinceAt, untilAt)
	if err != nil {
		return "", err
	}

	pagerdutyTeam := request.Team
	if request.PdTeam != "" {
		pagerdutyTeam = strings.ToLower(request.PdTeam)
	}
	pages, err := fetchPages(pagerdutyTeam, request.Since, request.Until, request.TagFilters, request.AuthToken, request.Urgency, request.Replace)
	if err != nil {
		return "", err
	}

	for _, p := range pages {
		for _, i := range incidents {
			if p.createdAt.After(i.createdAt.Add(-15*time.Minute)) &&
				p.createdAt.Before(i.resolvedAt) {
				i.pages = append(i.pages, p)
				p.incidentIDs = append(p.incidentIDs, i.id)
			}
		}
	}

	var md markdown

	report := strings.Builder{}

	title := strings.Title(fmt.Sprintf("%s On-Call Report %s", request.Team, request.Until))
	report.WriteString("---\n")
	report.WriteString(fmt.Sprintf("title: %s\n", title))
	report.WriteString("---\n")

	md.para(fmt.Sprintf("Report for %s - %s: total incidents - %d, total pages - %d", request.Since, request.Until, len(incidents), len(pages)))

	timeFormat := "2006-01-02 @15:04:05"
	for _, i := range incidents {

		when := i.createdAt.Local().Format(timeFormat)
		md.heading(3, link(fmt.Sprintf("%s | %s | %s | %s", i.sev, i.id, i.title, when), i.link))
		md.heading(4, fmt.Sprintf("IC: %s", i.commanderEmail))
		md.heading(4, "Root cause")
		md.para("  " + i.rootCause)
		md.heading(4, "Summary")
		md.para("  " + i.summary)
		if len(i.customerImpactScope) != 0 {
			md.heading(4, fmt.Sprintf("Customer impact (%s)", i.customerImpactDuration.String()))
			md.para("  " + i.customerImpactScope)
		}
		md.heading(4, "PagerDuty pages")
		for _, p := range i.pages {
			md.unordered(1, link(p.createdAt.Local().Format(timeFormat)+" "+p.title, p.link))
		}
		md.br()

		md.heading(4, "Action taken")
		md.para(filloutPlaceholder)
		md.heading(4, "Follow-up")
		md.unordered(1, "**Happened before/common theme**")
		md.para(filloutPlaceholder)
		md.unordered(1, "**How can we prevent it**")
		md.para(filloutPlaceholder)
		md.unordered(1, "**Runbooks**")
		md.para(filloutPlaceholder)
		md.unordered(1, "**Related PRs**")
		md.para(filloutPlaceholder)
		md.unordered(1, "**Action items**")
		md.para(filloutPlaceholder)
	}

	md.heading(3, "Other Pages")

	for _, p := range pages {
		if len(p.incidentIDs) != 0 {
			continue
		}
		md.unordered(1, link(p.createdAt.Local().Format(timeFormat)+" "+p.title, p.link))
		md.unordered(2, fmt.Sprintf("**Ack'ed by**: %s", strings.Join(p.responders, ", ")))
		if len(p.notes) != 0 {
			md.unordered(2, "**Notes**:")
			for _, n := range p.notes {
				if n.userEmail != "" {
					md.unordered(3, fmt.Sprintf("**%s**: %s", n.userEmail, n.content))
				} else {
					md.unordered(3, n.content)
				}
			}
			md.br()
		}
		md.unordered(2, "**Action taken**: "+filloutPlaceholder)
		md.unordered(2, "**Follow-up**: "+filloutPlaceholder)
	}

	report.WriteString(md.String())
	return report.String(), nil
}

func parseDates(since, until string) (sinceAt, untilAt time.Time, err error) {
	format := "2006-01-02"
	sinceAt, err = time.Parse(format, since)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to parse --since: %v", err)
		return time.Time{}, time.Time{}, errors.New(errMsg)
	}
	untilAt, err = time.Parse(format, until)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to parse --until: %v", err)
		return time.Time{}, time.Time{}, errors.New(errMsg)
	}
	if untilAt.Before(sinceAt) {
		errMsg := fmt.Sprintf("--since must start before --until. --since: %s, --until: %s", since, until)
		return time.Time{}, time.Time{}, errors.New(errMsg)
	}

	return sinceAt, untilAt, nil
}
