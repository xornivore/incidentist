package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
)

const (
	YYYYMMDD = "2006-01-02"
)

// ConfluencePage represents the JSON payload to create a new Confluence page
type ConfluencePage struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	Space struct {
		Key string `json:"key"`
	} `json:"space"`
	Ancestors []struct { // Add this field to specify the parent page
		ID string `json:"id"`
	} `json:"ancestors,omitempty"`
	Body struct {
		Storage struct {
			Value          string `json:"value"`
			Representation string `json:"representation"`
		} `json:"storage"`
	} `json:"body"`
}

type UploadRequest struct {
	ConfluenceSubdomain string
	ConfluenceUsername  string
	ConfluenceToken     string
	SpaceKey            string
	ParentId            string
	MarkdownContent     string
}

// pruneMarkdownTitle removes the title header from the markdown, if found.
// It expects the markdown to look like:
// ---
// title: Some Title
// ---
// Some content
func pruneMarkdownTitle(content string) (string, string) {
	r := regexp.MustCompile(`---\ntitle: (.*)\n---\n`)
	title := ""
	// Find match groups
	matches := r.FindStringSubmatch(content)
	if matches != nil && len(matches) > 1 {
		title = matches[1]
	}

	// Remove the entire match from the original string
	return r.ReplaceAllString(content, ""), title
}

// convertMarkdown converts Markdown format into HTML, which is expected by Confluence
func convertMarkdown(s string) (string, error) {
	renderOptions := []renderer.Option{
		html.WithXHTML(),
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(renderOptions...),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(s), &buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Upload creates a new Confluence page with the given details
func Upload(request UploadRequest) error {
	content, title := pruneMarkdownTitle(request.MarkdownContent)
	content, err := convertMarkdown(content)
	if err != nil {
		return fmt.Errorf("error converting markdown: %v", err)
	}

	// Try to come up with some title if we couldn't parse one
	if title == "" {
		title = fmt.Sprintf("On-Call Report %s", time.Now().Format(YYYYMMDD))
	}

	baseURL := fmt.Sprintf("https://%s.atlassian.net/wiki/rest/api/content", request.ConfluenceSubdomain)
	// Prepare the page payload
	newPage := ConfluencePage{
		Type:  "page",
		Title: title,
	}
	if request.ParentId != "" {
		newPage.Ancestors = []struct {
			ID string `json:"id"`
		}{{ID: request.ParentId}}
	}

	newPage.Space.Key = request.SpaceKey
	newPage.Body.Storage.Value = content
	newPage.Body.Storage.Representation = "storage"

	pageData, err := json.Marshal(newPage)
	if err != nil {
		return fmt.Errorf("error marshalling json: %v", err)
	}

	httpReq, err := http.NewRequest("POST", baseURL, bytes.NewBuffer(pageData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.SetBasicAuth(request.ConfluenceUsername, request.ConfluenceToken)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("error making request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error response body: %s", string(body))
		return fmt.Errorf("failed to create page, status code: %d", resp.StatusCode)
	}
	return nil
}
