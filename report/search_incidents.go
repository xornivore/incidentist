package report

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	neturl "net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	datadog "github.com/DataDog/datadog-api-client-go/api/v2/datadog"
)

var (
	jsonCheck = regexp.MustCompile(`(?i:(?:application|text)/(?:vnd\.[^;]+\+)?json)`)
	xmlCheck  = regexp.MustCompile(`(?i:(?:application|text)/xml)`)
)

// GenericOpenAPIError Provides access to the body, error and model on returned errors.
type GenericOpenAPIError struct {
	body  []byte
	error string
	model interface{}
}

// Error returns non-empty string if there was an error.
func (e GenericOpenAPIError) Error() string {
	return e.error
}

// Body returns the raw bytes of the response
func (e GenericOpenAPIError) Body() []byte {
	return e.body
}

// Model returns the unpacked model of the error
func (e GenericOpenAPIError) Model() interface{} {
	return e.model
}

// parameterToString convert interface{} parameters to string, using a delimiter if format is provided.
func parameterToString(obj interface{}, collectionFormat string) string {
	var delimiter string

	switch collectionFormat {
	case "pipes":
		delimiter = "|"
	case "ssv":
		delimiter = " "
	case "tsv":
		delimiter = "\t"
	case "csv":
		delimiter = ","
	}

	if reflect.TypeOf(obj).Kind() == reflect.Slice {
		return strings.Trim(strings.Replace(fmt.Sprint(obj), " ", delimiter, -1), "[]")
	} else if t, ok := obj.(time.Time); ok {
		return t.Format(time.RFC3339)
	}

	return fmt.Sprintf("%v", obj)
}

// selectHeaderContentType select a content type from the available list.
func selectHeaderContentType(contentTypes []string) string {
	if len(contentTypes) == 0 {
		return ""
	}
	if contains(contentTypes, "application/json") {
		return "application/json"
	}
	return contentTypes[0] // use the first content type specified in 'consumes'
}

// contains is a case insensitive match, finding needle in a haystack
func contains(haystack []string, needle string) bool {
	for _, a := range haystack {
		if strings.ToLower(a) == strings.ToLower(needle) {
			return true
		}
	}
	return false
}

// selectHeaderAccept join all accept types and return
func selectHeaderAccept(accepts []string) string {
	if len(accepts) == 0 {
		return ""
	}

	if contains(accepts, "application/json") {
		return "application/json"
	}

	return strings.Join(accepts, ",")
}

func decode(v interface{}, b []byte, contentType string) (err error) {
	if len(b) == 0 {
		return nil
	}
	if s, ok := v.(*string); ok {
		*s = string(b)
		return nil
	}
	if xmlCheck.MatchString(contentType) {
		if err = xml.Unmarshal(b, v); err != nil {
			return err
		}
		return nil
	}
	if jsonCheck.MatchString(contentType) {
		if actualObj, ok := v.(interface{ GetActualInstance() interface{} }); ok { // oneOf, anyOf schemas
			if unmarshalObj, ok := actualObj.(interface{ UnmarshalJSON([]byte) error }); ok { // make sure it has UnmarshalJSON defined
				if err = unmarshalObj.UnmarshalJSON(b); err != nil {
					return err
				}
			} else {
				return errors.New("Unknown type with GetActualInstance but no unmarshalObj.UnmarshalJSON defined")
			}
		} else if err = json.Unmarshal(b, v); err != nil { // simple model
			return err
		}
		return nil
	}
	return errors.New("undefined response type")
}

type searchRequest struct {
	createdAfter  *int64
	createdBefore *int64
	tags          []string
	pageSize      *int
	pageOffset    *int
}

// IncidentsResponse Response with a list of incidents.
type IncidentsSearchResponse struct {
	// An array of incidents.
	Incidents *[]IncidentResponseData `json:"included,omitempty"`
}

// IncidentResponseData Incident data from a response.
type IncidentResponseData struct {
	Attributes *IncidentResponseAttributes `json:"attributes,omitempty"`
	// The incident's ID.
	Id            string                                 `json:"id"`
	Relationships *datadog.IncidentResponseRelationships `json:"relationships,omitempty"`
	Type          datadog.IncidentType                   `json:"type"`
}

type IncidentResponseDataCommander struct {
	User *datadog.User `json:"data,omitempty"`
}

// IncidentResponseAttributes The incident's attributes from a response.
type IncidentResponseAttributes struct {
	// Timestamp when the incident was created.
	Created *time.Time `json:"created,omitempty"`
	// Commander is the person taking the incident [TODO: Added - missing in the SDK]
	Commander *IncidentResponseDataCommander `json:"commander,omitempty"`
	// Length of the incident's customer impact in seconds. Equals the difference between `customer_impact_start` and `customer_impact_end`.
	CustomerImpactDuration *int64 `json:"customer_impact_duration,omitempty"`
	// Timestamp when customers were no longer impacted by the incident.
	CustomerImpactEnd datadog.NullableTime `json:"customer_impact_end,omitempty"`
	// A summary of the impact customers experienced during the incident.
	CustomerImpactScope datadog.NullableString `json:"customer_impact_scope,omitempty"`
	// Timestamp when customers began being impacted by the incident.
	CustomerImpactStart datadog.NullableTime `json:"customer_impact_start,omitempty"`
	// A flag indicating whether the incident caused customer impact.
	CustomerImpacted *bool `json:"customer_impacted,omitempty"`
	// Timestamp when the incident was detected.
	Detected datadog.NullableTime `json:"detected,omitempty"`
	// A condensed view of the user-defined fields attached to incidents.
	Fields *map[string]datadog.IncidentFieldAttributes `json:"fields,omitempty"`
	// Timestamp when the incident was last modified.
	Modified *time.Time `json:"modified,omitempty"`
	// Notification handles that will be notified of the incident during update.
	NotificationHandles []datadog.IncidentNotificationHandle `json:"notification_handles,omitempty"`
	// The UUID of the postmortem object attached to the incident.
	PostmortemId *string `json:"postmortem_id,omitempty"`
	// The monotonically increasing integer ID for the incident.
	PublicId *int64 `json:"public_id,omitempty"`
	// Timestamp when the incident's state was set to resolved.
	Resolved datadog.NullableTime `json:"resolved,omitempty"`
	// The amount of time in seconds to detect the incident. Equals the difference between `customer_impact_start` and `detected`.
	TimeToDetect *int64 `json:"time_to_detect,omitempty"`
	// The amount of time in seconds to call incident after detection. Equals the difference of `detected` and `created`.
	TimeToInternalResponse *int64 `json:"time_to_internal_response,omitempty"`
	// The amount of time in seconds to resolve customer impact after detecting the issue. Equals the difference between `customer_impact_end` and `detected`.
	TimeToRepair *int64 `json:"time_to_repair,omitempty"`
	// The amount of time in seconds to resolve the incident after it was created. Equals the difference between `created` and `resolved`.
	TimeToResolve *int64 `json:"time_to_resolve,omitempty"`
	// The title of the incident, which summarizes what happened.
	Title string `json:"title"`
}

func (a *IncidentResponseAttributes) GetPublicId() int64 {
	if a.PublicId != nil {
		return *a.PublicId
	}
	return 0
}
func (a *IncidentResponseAttributes) GetFields() map[string]datadog.IncidentFieldAttributes {
	if a.Fields != nil {
		return *a.Fields
	}
	return map[string]datadog.IncidentFieldAttributes{}
}

/*
 * Execute executes the request
 * @return IncidentsSearchResponse
 */
func searchIncidents(ctx context.Context, client *datadog.APIClient, r *searchRequest) (IncidentsSearchResponse, *nethttp.Response, error) {
	var (
		localVarHTTPMethod   = nethttp.MethodGet
		localVarPostBody     interface{}
		localVarFormFileName string
		localVarFileName     string
		localVarFileBytes    []byte
		localVarReturnValue  IncidentsSearchResponse
	)

	// operationId := "SearchIncidents"

	localBasePath, err := client.GetConfig().ServerURLWithContext(ctx, "IncidentsApiService.SearchIncidents")
	if err != nil {
		return localVarReturnValue, nil, GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/api/v2/incidents/search"

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := neturl.Values{}
	localVarFormParams := neturl.Values{}

	var queryOpts []string

	if r.createdBefore != nil {
		queryOpts = append(queryOpts, fmt.Sprintf("created_before:%d", *r.createdBefore))
	}
	if r.createdAfter != nil {
		queryOpts = append(queryOpts, fmt.Sprintf("created_after:%d", *r.createdAfter))
	}
	queryOpts = append(queryOpts, r.tags...)

	if len(queryOpts) > 0 {
		localVarQueryParams.Add("query", strings.Join(queryOpts, " "))
	}

	if r.pageSize != nil {
		localVarQueryParams.Add("page[size]", parameterToString(*r.pageSize, ""))
	}
	if r.pageOffset != nil {
		localVarQueryParams.Add("page[offset]", parameterToString(*r.pageOffset, ""))
	}

	localVarQueryParams.Add("filter[field_type]", "all")
	localVarQueryParams.Add("sort", "created")

	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}

	// Set Operation-ID header for telemetry
	localVarHeaderParams["DD-OPERATION-ID"] = "SearchIncidents"

	if ctx != nil {
		// API Key Authentication
		if auth, ok := ctx.Value(datadog.ContextAPIKeys).(map[string]datadog.APIKey); ok {
			if apiKey, ok := auth["apiKeyAuth"]; ok {
				var key string
				if apiKey.Prefix != "" {
					key = apiKey.Prefix + " " + apiKey.Key
				} else {
					key = apiKey.Key
				}
				localVarHeaderParams["DD-API-KEY"] = key
			}
		}
	}
	if ctx != nil {
		// API Key Authentication
		if auth, ok := ctx.Value(datadog.ContextAPIKeys).(map[string]datadog.APIKey); ok {
			if apiKey, ok := auth["appKeyAuth"]; ok {
				var key string
				if apiKey.Prefix != "" {
					key = apiKey.Prefix + " " + apiKey.Key
				} else {
					key = apiKey.Key
				}
				localVarHeaderParams["DD-APPLICATION-KEY"] = key
			}
		}
	}
	req, err := client.PrepareRequest(ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, localVarFormFileName, localVarFileName, localVarFileBytes)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := client.CallAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v datadog.APIErrorResponse
			err = decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 401 {
			var v datadog.APIErrorResponse
			err = decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 403 {
			var v datadog.APIErrorResponse
			err = decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v datadog.APIErrorResponse
			err = decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
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

		incident := &incident{
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
		}
		if i.Attributes.Resolved.IsSet() && i.Attributes.Resolved.Get() != nil {
			incident.resolvedAt = *i.Attributes.Resolved.Get()
		}

		incidents = append(incidents, incident)

	}
	byCreatedAt := func(i, j int) bool {
		return incidents[i].createdAt.Before(incidents[j].createdAt)
	}
	sort.Slice(incidents, byCreatedAt)
	return incidents, nil
}
