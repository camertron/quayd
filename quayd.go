package quayd

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"github.com/google/go-github/github"
)

var (
	// Context is the string that will be displayed when showing the commit
	// status.
	Context = "Docker Image"

	// DefaultStatusesRepository is the default StatusesRepository to use.
	DefaultStatusesRepository = &statusesRepository{}

	// DefaultTagger is the default Tagger to use.
	DefaultTagger = &tagger{}

	// DefaultTagResovler is the default TagResolver to use.
	DefaultTagResolver = &tagResolver{}

	// Default is the default Quayd to use.
	Default = &Quayd{}

	Statuses = map[string]string{
		"pending": "The Docker image is building",
		"success": "The Docker image was built",
		"failure": "The Docker image failed to build",
	}
)

// Status represents a GitHub Commit Status.
type Status struct {
	Repo        string
	Ref         string
	State       string
	Context     string
	TargetURL   string
	Description string
}

// StatusesRepository is an interface that can be implemented for creating
// Commit Statuses.
type StatusesRepository interface {
	// Create creates a GitHub Commit Status.
	Create(*Status) error
}

// statusesRepository is a fake implementation of the StatusesRepository
// interface.
type statusesRepository struct {
	statuses []*Status
}

// Create implements StatusesRepository Create.
func (r *statusesRepository) Create(status *Status) error {
	r.statuses = append(r.statuses, status)

	return nil
}

// Reset resets the collection of Statuses.
func (r *statusesRepository) Reset() {
	r.statuses = nil
}

// GitHubStatusesRepository is an implementation of the StatusesRepository
// interface backed by a github.Client.
type GitHubStatusesRepository struct {
	RepositoriesService interface {
		CreateStatus(owner, repo, ref string, status *github.RepoStatus) (*github.RepoStatus, *github.Response, error)
	}
}

// Create implements StatusesRepository Create.
func (r *GitHubStatusesRepository) Create(status *Status) error {

	st := &github.RepoStatus{
		State:       &status.State,
		TargetURL:   &status.TargetURL,
		Context:     &status.Context,
		Description: &status.Description,
	}

	// Split `owner/repo` into ["owner", "repo"].
	c := strings.Split(status.Repo, "/")

	_, _, err := r.RepositoriesService.CreateStatus(
		c[0],
		c[1],
		status.Ref,
		st,
	)
	return err
}

// Tagger is an interface for tagging a docker image with a tag.
type Tagger interface {
	// Tag tags the imageID with the given tag.
	Tag(repo, imageID, tag string) error
}

// tagger is a fake implementation of the Tagger interface.
type tagger struct {
}

// Tag implements Tagger Tag.
func (t *tagger) Tag(repo, imageID, tag string) error {
	return nil
}

// DockerRegistryTagger is a Tagger implementation that can tag a
// docker image by using the docker registry api
type DockerRegistryTagger struct {
	registry string
	username string
	password string
}

func (dt *DockerRegistryTagger) Tag(repo, imageID, tag string) error {
	req, err := http.NewRequest("PUT",
		"https://"+dt.registry+"/v1/repositories/"+repo+"/tags/"+tag,
		strings.NewReader(`"`+imageID+`"`))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	req.SetBasicAuth(dt.username, dt.password)

	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode >= 300 {
		return errors.New("Unsuccessful Request: " + resp.Status)
	}

	return err
}

// TagResolver resolves a docker tag to an image id.
type TagResolver interface {
	Resolve(repo, tag string) (string, error)
}

// tagResolver is a fake implementation of the TagResolver interface.
type tagResolver struct{}

func (r *tagResolver) Resolve(repo, tag string) (string, error) {
	return "", nil
}

// DockerTagResolver is an implementation of the TagResolver that resolves an
// image tag to a docker image id, using the docker api.
type DockerRegistryTagResolver struct {
	registry string
}

func (r *DockerRegistryTagResolver) Resolve(repo, tag string) (string, error) {
	resp, err := http.Get("https://" + r.registry + "/v1/repositories/" + repo + "/tags/" + tag)
	if err != nil {
		return "", err
	}
	var imageID string
	if err := json.NewDecoder(resp.Body).Decode(&imageID); err != nil {
		return "", err
	}
	return imageID, nil
}

// Quayd provides a Handle method for adding a GitHub Commit Status and tagging
// the docker image.
type Quayd struct {
	StatusesRepository
	Tagger
	TagResolver
}

// New returns a new Quayd instance backed by GitHub implementations.
func New(token, registryAuth string) *Quayd {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(oauth2.NoContext, ts)
	gh := github.NewClient(tc)

	auth := strings.Split(registryAuth, ":")
	return &Quayd{
		StatusesRepository: &GitHubStatusesRepository{gh.Repositories},
		TagResolver:        &DockerRegistryTagResolver{registry: "quay.io"},
		Tagger: &DockerRegistryTagger{registry: "quay.io",
			username: auth[0],
			password: auth[1]},
	}
}

// Handle resolves the ref to a full 40 character sha, then creates a new GitHub
// Commit Status for that sha.
func (q *Quayd) Handle(repo, commit, url, state string) error {
	return q.statusesRepository().Create(&Status{
		Repo:        repo,
		TargetURL:   url,
		Ref:         commit,
		State:       state,
		Description: Statuses[state],
		Context:     Context,
	})
}

// LoadImageTags locates a build from its repo and tag and adds
// tags for the Image ID as well as the Git SHA since the docker
// registry does not currently support puling a docker image by its
// immutable identifier, only by a tag
func (q *Quayd) LoadImageTags(tag, repo, commit string) error {
	// Something that resolves the `tag` into an image id.
	imageID, err := q.tagResolver().Resolve(repo, tag)
	if err != nil {
		return err
	}

	if err := q.tagger().Tag(repo, imageID, commit); err != nil {
		return err
	}

	return q.tagger().Tag(repo, imageID, imageID)
}

func (q *Quayd) statusesRepository() StatusesRepository {
	if q.StatusesRepository == nil {
		return DefaultStatusesRepository
	}

	return q.StatusesRepository
}

func (q *Quayd) tagger() Tagger {
	if q.Tagger == nil {
		q.Tagger = DefaultTagger
	}

	return q.Tagger
}

func (q *Quayd) tagResolver() TagResolver {
	if q.TagResolver == nil {
		q.TagResolver = DefaultTagResolver
	}

	return q.TagResolver
}
