package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type projectHeaderTransport struct{ base http.RoundTripper }

func (h projectHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("X-Ardvi-Project", "00000000-0000-4000-8000-000000000000")
	return h.base.RoundTrip(request)
}

type listedSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
}

type listedSkills struct {
	Skills    []listedSkill     `json:"skills"`
	Revisions map[string]string `json:"revisions"`
	Next      string            `json:"next_cursor,omitempty"`
}

func printSkills(value listedSkills, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(value)
	}
	groups := map[string][]string{}
	for _, skill := range value.Skills {
		groups[skill.Source] = append(groups[skill.Source], skill.Name)
	}
	sources := make([]string, 0, len(groups))
	for source := range groups {
		sources = append(sources, source)
	}
	for source := range value.Revisions {
		if _, exists := groups[source]; !exists {
			sources = append(sources, source)
		}
	}
	sort.Strings(sources)
	for _, source := range sources {
		sort.Strings(groups[source])
		revision := value.Revisions[source]
		if revision == "" {
			revision = "built-in"
		}
		fmt.Printf("%s (%s)\n", source, revision)
		for _, name := range groups[source] {
			fmt.Printf("  %s\n", name)
		}
		if len(groups[source]) == 0 {
			fmt.Println("  (persona-only managed source)")
		}
	}
	return nil
}

func skillsCommand(args []string) error {
	if len(args) > 0 && args[0] == "update" {
		return installRuntime(args[1:], true)
	}
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: ardvi skills list [--json] [--url URL] | ardvi skills update")
	}
	f := flag.NewFlagSet("skills list", flag.ContinueOnError)
	jsonOutput := f.Bool("json", false, "print JSON")
	endpoint := f.String("url", "http://127.0.0.1:8765/mcp", "Ardvi MCP URL")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "ardvi-cli", Version: version}, nil)
	httpClient := &http.Client{Transport: projectHeaderTransport{base: http.DefaultTransport}}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: *endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	value := listedSkills{Revisions: map[string]string{}}
	seen := map[string]bool{}
	for cursor := ""; ; {
		arguments := map[string]any{"limit": 100}
		if cursor != "" {
			if seen[cursor] {
				return errors.New("skills_list returned a repeated cursor")
			}
			seen[cursor] = true
			arguments["cursor"] = cursor
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "skills_list", Arguments: arguments})
		if err != nil {
			return err
		}
		if result.IsError {
			return result.GetError()
		}
		b, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return err
		}
		var page listedSkills
		if err = json.Unmarshal(b, &page); err != nil {
			return err
		}
		value.Skills = append(value.Skills, page.Skills...)
		for source, revision := range page.Revisions {
			value.Revisions[source] = revision
		}
		cursor = page.Next
		if cursor == "" {
			break
		}
	}
	return printSkills(value, *jsonOutput)
}
