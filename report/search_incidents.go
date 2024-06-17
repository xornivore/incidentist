package report

import (
	"context"
	"fmt"
	nethttp "net/http"
	"os"
	"sort"
	"strings"
	"time"

	datadog "github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	datadogV2 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

type searchRequest struct {
	createdAfter  *int64
	createdBefore *int64
	tags          []string
}

func searchIncidents(ctx context.Context, client *datadog.APIClient, r *searchRequest) (datadogV2.IncidentSearchResponse, *nethttp.Response, error) {
	var queryOpts []string
	if r.createdBefore != nil {
		queryOpts = append(queryOpts, fmt.Sprintf("created_before:%d", *r.createdBefore))
	}
	if r.createdAfter != nil {
		queryOpts = append(queryOpts, fmt.Sprintf("created_after:%d", *r.createdAfter))
	}
	queryOpts = append(queryOpts, r.tags...)
	query := strings.Join(queryOpts, " AND ")

	queryParams := datadogV2.NewSearchIncidentsOptionalParameters().WithSort(datadogV2.INCIDENTSEARCHSORTORDER_CREATED_ASCENDING)

	api := datadogV2.NewIncidentsApi(client)
	return api.SearchIncidents(ctx, query, *queryParams)
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

func fetchIncidents(teams []string, ddApiKey, ddAppKey string, since, until time.Time) ([]*incident, error) {
	ctx := getDatadogAPIContext(ddApiKey, ddAppKey)
	configuration := datadog.NewConfiguration()
	configuration.SetUnstableOperationEnabled("v2.SearchIncidents", true)
	apiClient := datadog.NewAPIClient(configuration)

	createdAfter := since.UTC().Unix()
	createdBefore := until.UTC().Unix()
	req := &searchRequest{
		createdAfter:  &createdAfter,
		createdBefore: &createdBefore,
		tags: []string{
			getTeamFilter(teams),
		},
	}

	resp, r, err := searchIncidents(ctx, apiClient, req)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
		return nil, fmt.Errorf("Error when searching for incidents: %w", err)
	}

	if resp.Data.Attributes == nil || resp.Data.Attributes.Incidents == nil {
		return nil, nil
	}

	// The raw API response actually contains the incident commander embedded in the incidents, but the SDK doesn't expose it, as this is technically not JSON:API compliant. The SDK only exposes an ID in the relationships.
	// Instead we extract the incident commander data from the facets and use the commander UUID provided to map back to the full commander data
	commanders := getIncidentCommanderMap(resp)

	var incidents []*incident
	for _, i := range resp.Data.Attributes.Incidents {
		data := i.Data
		if data.Type != "incidents" {
			continue
		}
		id := data.Attributes.GetPublicId()

		var commander datadogV2.IncidentSearchResponseUserFacetData
		if commanderData := data.Relationships.CommanderUser.Data.Get(); commanderData != nil {
			commanderId := commanderData.Id
			commander = commanders[commanderId]
		}

		incident := &incident{
			id:                     fmt.Sprintf("#incident-%d", id),
			title:                  data.Attributes.Title,
			link:                   fmt.Sprintf("https://app.datadoghq.com/incidents/%d", id),
			commander:              *commander.Name,
			commanderEmail:         *commander.Email,
			sev:                    data.Attributes.GetFields()["severity"].IncidentFieldAttributesSingleValue.GetValue(),
			rootCause:              data.Attributes.GetFields()["root_cause"].IncidentFieldAttributesSingleValue.GetValue(),
			summary:                data.Attributes.GetFields()["summary"].IncidentFieldAttributesSingleValue.GetValue(),
			customerImpactScope:    *data.Attributes.CustomerImpactScope.Get(),
			customerImpactDuration: time.Duration(*data.Attributes.CustomerImpactDuration * int64(time.Second)),
			createdAt:              *data.Attributes.Created,
		}
		if data.Attributes.Resolved.IsSet() && data.Attributes.Resolved.Get() != nil {
			incident.resolvedAt = *data.Attributes.Resolved.Get()
		}

		incidents = append(incidents, incident)

	}
	byCreatedAt := func(i, j int) bool {
		return incidents[i].createdAt.Before(incidents[j].createdAt)
	}
	sort.Slice(incidents, byCreatedAt)
	return incidents, nil
}

func getDatadogAPIContext(ddApiKey, ddAppKey string) context.Context {
	ctx := context.Background()

	// always load incidents from US1
	ctx = context.WithValue(
		ctx,
		datadog.ContextServerVariables,
		map[string]string{"site": "datadoghq.com"},
	)

	keys := make(map[string]datadog.APIKey)
	keys["apiKeyAuth"] = datadog.APIKey{Key: ddApiKey}
	keys["appKeyAuth"] = datadog.APIKey{Key: ddAppKey}
	ctx = context.WithValue(
		ctx,
		datadog.ContextAPIKeys,
		keys,
	)
	return ctx
}

func getIncidentCommanderMap(searchResponse datadogV2.IncidentSearchResponse) map[string]datadogV2.IncidentSearchResponseUserFacetData {
	commanders := searchResponse.Data.Attributes.Facets.Commander
	commanderMap := make(map[string]datadogV2.IncidentSearchResponseUserFacetData, len(commanders))
	for _, commander := range commanders {
		commanderMap[*commander.Uuid] = commander
	}
	return commanderMap
}

func getTeamFilter(teams []string) string {
	if len(teams) == 1 {
		return fmt.Sprintf("teams:%s", teams[0])
	}
	return "teams:(" + strings.Join(teams, " OR ") + ")"
}
