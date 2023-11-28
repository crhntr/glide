package globalentry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

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

func (client *Client) Teams(ctx context.Context) ([]Team, error) {
	return listResource[Team](ctx, client, "teams")
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

func (client *Client) Pipelines(ctx context.Context, team string) ([]Pipeline, error) {
	return listResource[Pipeline](ctx, client, "teams", team, "pipelines")
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
}

type BuildInput struct {
	Name     string `json:"name"`
	Resource string `json:"resource"`
	Trigger  bool   `json:"trigger"`
}

func (client *Client) Jobs(ctx context.Context, team, pipeline string) ([]Job, error) {
	return listResource[Job](ctx, client, "teams", team, "pipelines", pipeline, "jobs")
}

type httpError struct {
	StatusCode int
	Body       []byte
}

func (err *httpError) Error() string {
	return fmt.Sprintf("http error: %d: %s", err.StatusCode, err.Body)
}

func listResource[T any](ctx context.Context, client *Client, segments ...string) ([]T, error) {
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
