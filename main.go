package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/xornivore/incidentist/report"
)

var (
	authToken  = kingpin.Flag("auth", "Auth token").String()
	team       = kingpin.Flag("team", "Team").Required().String()
	pdTeam     = kingpin.Flag("pd-team", "Team in PagerDuty if different from Team").String()
	since      = kingpin.Flag("since", "Since date/time").Required().String()
	until      = kingpin.Flag("until", "Until date/time").Required().String()
	urgency    = kingpin.Flag("urgency", "Urgency").Default("high").String()
	replace    = kingpin.Flag("replace", "Replace titles with regex").Strings()
	tagFilters = kingpin.Flag("tags", "Filter PagerDuty incidents by Datadog tags").Strings()
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

	ddApiKey := os.Getenv("DD_API_KEY")
	if ddApiKey == "" {
		exit("missing datadog api key (DD_API_KEY)")
	}
	
	ddAppKey := os.Getenv("DD_APP_KEY")
	if ddAppKey == "" {
		exit("missing datadog app key (DD_APP_KEY)")
	}

	request := report.GenerateReportRequest{
		Team:       *team,
		PdTeam:     *pdTeam,
		Since:      *since,
		Until:      *until,
		TagFilters: *tagFilters,
		AuthToken:  *authToken,
		Urgency:    *urgency,
		Replace:    *replace,
		DdApiKey:   ddApiKey,
		DdAppKey:   ddAppKey,
	}

	report, err := report.GenerateReport(request)
	if err != nil {
		exit("error generating report: %v", err)
	} else {
		fmt.Println(report)
	}

}
