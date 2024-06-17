package report

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PagerDuty/go-pagerduty"
)

type pageNote struct {
	content   string
	userName  string
	userEmail string
}

type page struct {
	title       string
	link        string
	createdAt   time.Time
	incidentIDs []string
	responders  []string
	notes       []pageNote
}

func fetchPages(pagerdutyTeams []string, since, until string, tagFilters []string, authToken string, urgency string, replace []string) ([]*page, error) {
	client := pagerduty.NewClient(authToken)

	regexReplace, err := getRegexReplace(replace)
	if err != nil {
		return nil, err
	}

	teamIDs, err := getTeamIds(pagerdutyTeams, client)
	if err != nil {
		return nil, err
	}

	incResp, err := client.ListIncidentsWithContext(context.Background(), pagerduty.ListIncidentsOptions{
		Limit:     1000,
		TeamIDs:   teamIDs,
		Since:     since,
		Until:     until,
		Urgencies: []string{urgency},
	})

	if err != nil {
		return nil, err
	}

	var pages []*page

	for _, p := range incResp.Incidents {
		matched, err := pagerdutyIncidentMatchesTags(client, p.ID, tagFilters)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not fetch tags for incident %s, skipping: %v\n", p.ID, err)
			continue
		}
		if !matched {
			continue
		}

		title := p.Title
		for r, replace := range regexReplace {
			title = r.ReplaceAllString(title, replace)
		}
		createdAt, _ := time.Parse(time.RFC3339, p.CreatedAt)

		notes, err := client.ListIncidentNotesWithContext(context.Background(), p.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not fetch notes for incident %s, skipping: %v\n", p.ID, err)
			continue
		}

		var pageNotes []pageNote
		for _, n := range notes {
			note := pageNote{
				content: n.Content,
			}

			if u, err := client.GetUserWithContext(context.Background(), n.User.ID, pagerduty.GetUserOptions{}); err != nil {
				fmt.Fprintf(os.Stderr, "Could not fetch user %s, ignoring: %v\n", n.User.ID, err)
			} else {
				note.userName = u.Name
				note.userEmail = u.Email
			}
			pageNotes = append(pageNotes, note)
		}

		logs, _ := client.ListIncidentLogEntriesWithContext(context.Background(), p.ID, pagerduty.ListIncidentLogEntriesOptions{})

		var responders []string
		for _, l := range logs.LogEntries {

			for _, a := range l.Assignees {
				if a.Type != "user_reference" {
					continue
				}

				u, err := client.GetUserWithContext(context.Background(), a.ID, pagerduty.GetUserOptions{})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Could not fetch user %s, ignoring: %v\n", a.ID, err)
					continue
				}
				responders = append(responders, u.Email)
			}
		}

		pages = append(pages, &page{
			title:      p.Title,
			link:       p.HTMLURL,
			createdAt:  createdAt,
			responders: responders,
			notes:      pageNotes,
		})
	}
	return pages, nil
}

func getRegexReplace(replace []string) (map[*regexp.Regexp]string, error) {
	regexReplace := map[*regexp.Regexp]string{}

	for _, r := range replace {

		parts := strings.SplitN(strings.Trim(r, "/"), "/", 2)
		if len(parts) < 1 {
			errorMessage := fmt.Sprintf("Invalid regexp replacement %s", r)
			return nil, errors.New(errorMessage)
		}
		replaceWith := ""
		if len(parts) == 2 {
			replaceWith = parts[1]
		}
		regexReplace[regexp.MustCompile(parts[0])] = replaceWith
	}
	return regexReplace, nil
}

func pagerdutyIncidentMatchesTags(client *pagerduty.Client, incidentId string, tagFilters []string) (bool, error) {
	if tagFilters == nil || len(tagFilters) == 0 {
		return true, nil
	}

	alertsResp, err := client.ListIncidentAlerts(incidentId)
	if err != nil {
		return false, err
	}

	for _, a := range alertsResp.Alerts {
		alertTags := getTagsFromPagerdutyAlert(a)
		if alertTags == nil {
			continue
		}

		found := true
		for _, tagFilter := range tagFilters {
			if _, ok := alertTags[tagFilter]; !ok {
				found = false
				break
			}
		}

		if found {
			return true, nil
		}
	}

	return false, nil
}

func getTagsFromPagerdutyAlert(alert pagerduty.IncidentAlert) map[string]struct{} {
	details, ok := alert.Body["details"]
	if !ok {
		return nil
	}

	detailsMap, ok := details.(map[string]interface{})
	if !ok {
		return nil
	}

	tags, ok := detailsMap["tags"]
	if !ok {
		return nil
	}

	tagsString, ok := tags.(string)
	if !ok {
		return nil
	}

	tokens := strings.Split(tagsString, ",")
	alertTags := make(map[string]struct{})
	for _, token := range tokens {
		alertTags[strings.TrimSpace(token)] = struct{}{}
	}

	return alertTags
}

// getTeamIds searches for the pagerduty team ids given their team names
func getTeamIds(teams []string, client *pagerduty.Client) ([]string, error) {
	teamIDs := make([]string, 0, len(teams))
	errs := make([]error, 0, len(teams))
	for _, team := range teams {
		teamID, err := getTeamId(team, client)
		if err == nil {
			teamIDs = append(teamIDs, teamID)
		} else {
			errs = append(errs, err)
		}
	}

	if len(teamIDs) == 0 {
		return nil, fmt.Errorf("could not find any team IDs: %v", errs)
	}

	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "WARN: some teams could not be found: %v", errs)
	}

	return teamIDs, nil
}

// getTeamId searches for the pagerduty team id given its team name
func getTeamId(name string, client *pagerduty.Client) (string, error) {
	var offset uint
	// Paginate through results until we find the team there are no more results
	for {
		response, err := client.ListTeams(pagerduty.ListTeamOptions{
			Offset: offset,
			Limit:  100, // PD only allows up to 100 results through the API
		})

		if err != nil {
			return "", fmt.Errorf("failed to list teams: %v", err)
		}

		for _, t := range response.Teams {
			if strings.ToLower(t.Name) == name {
				return t.ID, nil
			}
		}
		if !response.More {
			return "", fmt.Errorf("team %s not found", name)
		}

		offset += response.Limit
	}
}
