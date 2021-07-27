package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PagerDuty/go-pagerduty"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	authToken = kingpin.Flag("auth", "Auth token").String()
	team      = kingpin.Flag("team", "Team").Required().String()
	since     = kingpin.Flag("since", "Since date/time").Required().String()
	until     = kingpin.Flag("until", "Until date/time").Required().String()
	urgency   = kingpin.Flag("urgency", "Urgency").Default("high").String()
	envPrefix = kingpin.Flag("env-prefix", "Env prefix regex").Default(`^\[.*?\]`).String()
	replace   = kingpin.Flag("replace", "Replace titles with regex").Strings()
)

func error(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

func exit(format string, a ...interface{}) {
	error(format, a...)
	os.Exit(-1)
}

type page struct {
	createdAt time.Time
	link      string
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
	regexEnv := regexp.MustCompile(*envPrefix)

	client := pagerduty.NewClient(*authToken)

	teams, err := client.ListTeams(pagerduty.ListTeamOptions{
		APIListObject: pagerduty.APIListObject{
			Limit: 10000,
		},
	})
	if err != nil {
		exit("Failed to list teams: %v", err)
	}

	var teamID string
	for _, t := range teams.Teams {
		if strings.ToLower(t.Name) == *team {
			teamID = t.ID
			break
		}
	}

	if teamID == "" {
		exit("Team %s not found", *team)
	}

	incResp, err := client.ListIncidents(pagerduty.ListIncidentsOptions{
		APIListObject: pagerduty.APIListObject{
			Limit: 1000,
		},
		TeamIDs:   []string{teamID},
		Since:     *since,
		Until:     *until,
		Urgencies: []string{*urgency},
	})

	if err != nil {
		exit("Failed to list pages for team %s", *team)
	}

	type envMap map[string][]page
	pageMap := map[string]envMap{}

	type pagesWithTitle struct {
		title string
		pages envMap
	}
	var pages []pagesWithTitle

	for _, i := range incResp.Incidents {
		title := i.Title
		for r, replace := range regexReplace {
			title = r.ReplaceAllString(title, replace)
		}

		createdAt, _ := time.Parse(time.RFC3339, i.CreatedAt)

		env := regexEnv.FindString(title)
		title = strings.TrimSpace(title[len(env):])

		p := page{
			link:      i.HTMLURL,
			createdAt: createdAt,
		}

		if _, ok := pageMap[title]; !ok {
			pageMap[title] = envMap{}
			pages = append(pages, pagesWithTitle{
				title: title,
				pages: pageMap[title],
			})
		}
		pageMap[title][env] = append(pageMap[title][env], p)
	}

	var md markdown

	title := strings.Title(fmt.Sprintf("%s pages %s", *team, *until))

	fmt.Println("---")
	fmt.Printf("title: %s\n", title)
	fmt.Println("draft: false")
	fmt.Println("---")

	md.para(fmt.Sprintf("Report for %s - %s: total pages - %d", *since, *until, len(incResp.Incidents)))

	for _, entry := range pages {
		md.heading(3, entry.title)
		for e, pp := range entry.pages {
			md.unordered(1, fmt.Sprintf("**%s**:", e))
			for _, p := range pp {
				md.unordered(2, link(p.createdAt.Local().Format("2006-01-02 15:04:05"), p.link))
			}
			md.br()
			md.para("  Actions taken:")
			md.para("  Follow-up:")
		}
	}
	fmt.Print(md.String())
}
