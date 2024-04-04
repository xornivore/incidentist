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
	// Params for uploading the report
	confSubdomain = kingpin.Flag("subdomain", "Confluence subdomain").String()
	spaceKey      = kingpin.Flag("space", "Confluence space key").String()
	parentId      = kingpin.Flag("parent", "Confluence parent page id").String()
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

	var confUsername, confToken string
	doUpload := *confSubdomain != ""
	if doUpload {
		// Only check these credentials if we want to upload to confluence
		confUsername = os.Getenv("CONFLUENCE_USERNAME")
		if confUsername == "" {
			exit("missing confluence username (CONFLUENCE_USERNAME)")
		}

		confToken = os.Getenv("CONFLUENCE_API_TOKEN")
		if confToken == "" {
			exit("missing confluence auth token (CONFLUENCE_API_TOKEN)")
		}
		if *spaceKey == "" {
			exit("missing space key (--space-key)")
		}
	}

	generateRequest := report.GenerateRequest{
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

	content, err := report.Generate(generateRequest)

	if err != nil {
		exit("error generating report: %v", err)
	} else if doUpload {
		uploadRequest := report.UploadRequest{
			ConfluenceSubdomain: *confSubdomain,
			ConfluenceUsername:  confUsername,
			ConfluenceToken:     confToken,
			SpaceKey:            *spaceKey,
			ParentId:            *parentId,
			MarkdownContent:     content,
		}
		err = report.Upload(uploadRequest)
		if err != nil {
			exit("error uploading report: %v", err)
		} else {
			fmt.Println("Report uploaded successfully")
		}
	} else {
		// If not uploading, just dump to stderr.
		fmt.Println(content)
	}
}
