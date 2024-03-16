package glide

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vito/go-sse/sse"
	"golang.org/x/oauth2"
)

type Client struct {
	Client http.Client

	URL      string
	Username string
	Password string

	token atomic.Pointer[oauth2.Token]

	runSetupClient, runLoadEnvironment sync.Once
}

func (client *Client) Do(req *http.Request) (*http.Response, error) {
	client.runLoadEnvironment.Do(client.loadEnvironment)
	client.runSetupClient.Do(client.setupClient)
	return client.Client.Do(req)
}

func (client *Client) APIPath(segments ...string) string {
	client.runLoadEnvironment.Do(client.loadEnvironment)
	return client.URL + "/" + path.Join(append([]string{"api", "v1"}, segments...)...)
}

func (client *Client) loadEnvironment() {
	if value, isSet := os.LookupEnv("CONCOURSE_URL"); isSet && client.URL == "" {
		client.URL = value
	}
	if value, isSet := os.LookupEnv("CONCOURSE_USERNAME"); isSet && client.Username == "" {
		client.Username = value
	}
	if value, isSet := os.LookupEnv("CONCOURSE_PASSWORD"); isSet && client.Password == "" {
		client.Password = value
	}
}

func (client *Client) setupClient() {
	base := client.Client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Client = http.Client{
		Transport: &oauth2.Transport{
			Base:   base,
			Source: client,
		},
	}
}

func (client *Client) Token() (*oauth2.Token, error) {
	token := client.token.Load()
	if token == nil || !token.Valid() {
		var err error
		ctx := context.Background()
		token, err = skyMarshalToken(ctx, client.URL, client.Username, client.Password)
		if err != nil {
			return nil, err
		}
		client.token.Store(token)
	}
	return token, nil
}

func skyMarshalToken(ctx context.Context, host, username, password string) (*oauth2.Token, error) {
	config := skyMarshalOAuth2Configuration(host)
	return config.PasswordCredentialsToken(ctx, username, password)
}

func skyMarshalOAuth2Configuration(host string) oauth2.Config {
	return oauth2.Config{
		ClientID:     "fly",
		ClientSecret: "Zmx5",
		Endpoint: oauth2.Endpoint{
			TokenURL: host + "/sky/issuer/token",
		},
		Scopes: []string{"openid", "profile", "email", "federated:id", "groups"},
	}
}

func closeAndIgnoreErr(c io.Closer) {
	_ = c.Close()
}

type Team struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Pipeline struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Paused      bool   `json:"paused"`
	Public      bool   `json:"public"`
	Archived    bool   `json:"archived"`
	TeamName    string `json:"team_name"`
	LastUpdated int64  `json:"last_updated"`
}

func (pipeline Pipeline) LastUpdatedTime() time.Time {
	return time.Unix(pipeline.LastUpdated, 0)
}

type Resource struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	PipelineID   int    `json:"pipeline_id"`
	PipelineName string `json:"pipeline_name"`
	TeamName     string `json:"team_name"`
	LastChecked  int    `json:"last_checked"`
	Build        struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		Status       string `json:"status"`
		StartTime    int    `json:"start_time"`
		EndTime      int    `json:"end_time"`
		TeamName     string `json:"team_name"`
		PipelineId   int    `json:"pipeline_id"`
		PipelineName string `json:"pipeline_name"`
		Plan         struct {
			ID    string `json:"id"`
			Check struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"check"`
		} `json:"plan"`
	} `json:"build"`
}

type Job struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	TeamName        string   `json:"team_name"`
	PipelineID      int      `json:"pipeline_id"`
	PipelineName    string   `json:"pipeline_name"`
	FinishedBuild   Build    `json:"finished_build"`
	TransitionBuild Build    `json:"transition_build"`
	Groups          []string `json:"groups"`
	HasNewInputs    bool     `json:"has_new_inputs"`
}

type BuildInput struct {
	Name     string `json:"name"`
	Resource string `json:"resource"`
	Trigger  bool   `json:"trigger"`
}

type Build struct {
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	Status       string       `json:"status"`
	StartTime    int64        `json:"start_time"`
	EndTime      int64        `json:"end_time"`
	TeamName     string       `json:"team_name"`
	PipelineID   int          `json:"pipeline_id"`
	PipelineName string       `json:"pipeline_name"`
	JobName      string       `json:"job_name"`
	Inputs       []BuildInput `bson:"inputs"`
	URL          string       `json:"api_url"`
	CreatedBy    string       `json:"created_by,omitempty"`
}

type ResourceVersion struct {
	ID      int             `json:"id"`
	Version json.RawMessage `json:"version"`
	Enabled bool            `json:"enabled"`
}

type BuildEvent struct {
	Data  BuildEventData `json:"data"`
	Event string         `json:"event"`
}

type BuildEventData struct {
	Payload string          `json:"payload"`
	Time    int64           `json:"time"`
	Origin  json.RawMessage `json:"origin"`
	Message string          `json:"message"`
}

func (client *Client) Teams(ctx context.Context) ([]Team, error) {
	return getList[Team](ctx, client, "teams")
}

func (client *Client) Pipelines(ctx context.Context, team string) ([]Pipeline, error) {
	return getList[Pipeline](ctx, client, "teams", team, "pipelines")
}

func (client *Client) Resources(ctx context.Context, team, pipeline string) ([]Resource, error) {
	return getList[Resource](ctx, client, "teams", team, "pipelines", pipeline, "resources")
}

func (client *Client) ResourceVersions(ctx context.Context, team, pipeline, resource string) ([]ResourceVersion, error) {
	return getList[ResourceVersion](ctx, client, "teams", team, "pipelines", pipeline, "resources", resource, "versions")
}

func (client *Client) Jobs(ctx context.Context, team, pipeline string) ([]Job, error) {
	return getList[Job](ctx, client, "teams", team, "pipelines", pipeline, "jobs")
}

func (client *Client) JobBuilds(ctx context.Context, team, pipeline, job string) ([]Build, error) {
	return getList[Build](ctx, client, "teams", team, "pipelines", pipeline, "jobs", job, "builds")
}

func (client *Client) JobBuildsWithResourceVersion(ctx context.Context, team, pipeline, resource string, versionID int) ([]Build, error) {
	return getList[Build](ctx, client, "teams", team, "pipelines", pipeline, "resources", resource, "versions", strconv.Itoa(versionID), "input_to")
}

func (client *Client) BuildEvents(ctx context.Context, buildID int) (<-chan BuildEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.APIPath("builds", strconv.Itoa(buildID), "events"), nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, &httpError{StatusCode: res.StatusCode}
	}
	rc := sse.NewReadCloser(res.Body)
	c := make(chan BuildEvent)
	go sendBuildEvents(ctx, c, rc)
	return c, nil
}

func sendBuildEvents(ctx context.Context, c chan<- BuildEvent, rc *sse.ReadCloser) {
	defer close(c)
	for {
		if err := ctx.Err(); err != nil {
			closeAndIgnoreErr(rc)
			return
		}
		event, err := rc.Next()
		if err != nil || event.Name == "end" {
			return
		}
		var message BuildEvent
		if err := json.Unmarshal(event.Data, &message); err != nil {
			continue
		}
		c <- message
	}
}

func getList[T any](ctx context.Context, client *Client, segments ...string) ([]T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.APIPath(segments...), nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeAndIgnoreErr(res.Body)
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, &httpError{StatusCode: res.StatusCode, Body: body}
	}
	var result []T
	return result, json.Unmarshal(body, &result)
}

type httpError struct {
	StatusCode int
	Body       []byte
}

func (err *httpError) Error() string {
	return fmt.Sprintf("http error: %d: %s", err.StatusCode, err.Body)
}
