package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"gopkg.in/alecthomas/kingpin.v2"

	datadog "github.com/DataDog/datadog-api-client-go/api/v2/datadog"
)

var (
	authToken = kingpin.Flag("auth", "Auth token").String()
	team      = kingpin.Flag("team", "Team").Required().String()
	since     = kingpin.Flag("since", "Since date/time").Required().String()
	until     = kingpin.Flag("until", "Until date/time").Required().String()
	urgency   = kingpin.Flag("urgency", "Urgency").Default("high").String()
	replace   = kingpin.Flag("replace", "Replace titles with regex").Strings()
)

const (
	filloutPlaceholder = "  _TODO: please fill out_"
)

func errorf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

func exit(format string, a ...interface{}) {
	errorf(format, a...)
	os.Exit(-1)
}

func main() {

	kingpin.Parse()
	*team = strings.ToLower(*team)
	if *authToken == "" {
		*authToken = os.Getenv("PD_AUTH_TOKEN")
	}

	if *authToken == "" {
		exit("missing auth token (--auth or PD_AUTH_TOKEN)")
	}

	format := "2006-01-02"
	sinceAt, err := time.Parse(format, *since)
	if err != nil {
		exit("Failed to parse --since: %v", err)
	}
	untilAt, err := time.Parse(format, *until)
	if err != nil {
		exit("Failed to parse --until: %v", err)
	}

	incidents, err := fetchIncidents(*team, sinceAt, untilAt)
	if err != nil {
		exit("Failed to fetch incidents from Datadog: %v", err)
	}

	pages, err := fetchPages(*team, *since, *until)
	if err != nil {
		exit("Failed to fetch PagerDuty pages: %v", err)
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

	title := strings.Title(fmt.Sprintf("%s On-Call Report %s", *team, *until))

	fmt.Println("---")
	fmt.Printf("title: %s\n", title)
	fmt.Println("---")

	md.para(fmt.Sprintf("Report for %s - %s: total incidents - %d, total pages - %d", *since, *until, len(incidents), len(pages)))

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
		md.unordered(2, "**Action taken**: "+filloutPlaceholder)
		md.unordered(2, "**Follow-up**: "+filloutPlaceholder)
	}
	fmt.Print(md.String())
}

func getRegexReplace() map[*regexp.Regexp]string {
	regexReplace := map[*regexp.Regexp]string{}

	for _, r := range *replace {

		parts := strings.SplitN(strings.Trim(r, "/"), "/", 2)
		if len(parts) < 1 {
			exit("Invalid regexp replacement %s", r)
		}
		replaceWith := ""
		if len(parts) == 2 {
			replaceWith = parts[1]
		}
		regexReplace[regexp.MustCompile(parts[0])] = replaceWith
	}
	return regexReplace
}

func newIntPtr(v int) *int {
	return &v
}

func newUInt64(v uint64) *uint64 {
	return &v
}

type page struct {
	title       string
	link        string
	createdAt   time.Time
	incidentIDs []string
}

type incident struct {
	id                     string
	title                  string
	link                   string
	sev                    string
	commander              string
	commanderEmail         string
	rootCause              string
	summary                string
	customerImpactScope    string
	customerImpactDuration time.Duration
	createdAt              time.Time
	resolvedAt             time.Time
	pages                  []*page
}

func fetchPages(team, since, until string) ([]*page, error) {
	client := pagerduty.NewClient(*authToken)

	regexReplace := getRegexReplace()

	teams, err := client.ListTeams(pagerduty.ListTeamOptions{
		APIListObject: pagerduty.APIListObject{
			Limit: 10000,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list teams: %v", err)
	}

	var teamID string
	for _, t := range teams.Teams {
		if strings.ToLower(t.Name) == team {
			teamID = t.ID
			break
		}
	}

	if teamID == "" {
		return nil, fmt.Errorf("team %s not found", team)
	}

	incResp, err := client.ListIncidents(pagerduty.ListIncidentsOptions{
		APIListObject: pagerduty.APIListObject{
			Limit: 1000,
		},
		TeamIDs:   []string{teamID},
		Since:     since,
		Until:     until,
		Urgencies: []string{*urgency},
	})

	if err != nil {
		return nil, err
	}

	var pages []*page

	for _, p := range incResp.Incidents {
		title := p.Title
		for r, replace := range regexReplace {
			title = r.ReplaceAllString(title, replace)
		}
		createdAt, _ := time.Parse(time.RFC3339, p.CreatedAt)
		pages = append(pages, &page{
			title:     p.Title,
			link:      p.HTMLURL,
			createdAt: createdAt,
		})

	}
	return pages, nil
}

func fetchIncidents(team string, since, until time.Time) ([]*incident, error) {
	ctx := datadog.NewDefaultContext(context.Background())
	configuration := datadog.NewConfiguration()
	configuration.SetUnstableOperationEnabled("ListIncidents", true)
	apiClient := datadog.NewAPIClient(configuration)

	createdAfter := since.UTC().Unix()
	createdBefore := until.UTC().Unix()
	req := &searchRequest{
		createdAfter:  &createdAfter,
		createdBefore: &createdBefore,
		tags: []string{
			"teams:" + team,
		},
	}

	resp, r, err := searchIncidents(ctx, apiClient, req)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
		return nil, fmt.Errorf("Error when calling `IncidentsApi.SearchIncidents`: %w", err)
	}

	if resp.Incidents == nil {
		return nil, nil
	}

	var incidents []*incident
	for _, i := range *resp.Incidents {
		if i.Type != "incidents" {
			continue
		}
		id := i.Attributes.GetPublicId()

		commander := i.Attributes.Commander.User

		incidents = append(incidents, &incident{
			id:                     fmt.Sprintf("#incident-%d", id),
			title:                  i.Attributes.Title,
			link:                   fmt.Sprintf("https://app.datadoghq.com/incidents/%d", id),
			commander:              *commander.Attributes.Name.Get(),
			commanderEmail:         *commander.Attributes.Email,
			sev:                    i.Attributes.GetFields()["severity"].IncidentFieldAttributesSingleValue.GetValue(),
			rootCause:              i.Attributes.GetFields()["root_cause"].IncidentFieldAttributesSingleValue.GetValue(),
			summary:                i.Attributes.GetFields()["summary"].IncidentFieldAttributesSingleValue.GetValue(),
			customerImpactScope:    *i.Attributes.CustomerImpactScope.Get(),
			customerImpactDuration: time.Duration(*i.Attributes.CustomerImpactDuration * int64(time.Second)),
			createdAt:              *i.Attributes.Created,
			resolvedAt:             *i.Attributes.Resolved.Get(),
		})

	}
	return incidents, nil
}
