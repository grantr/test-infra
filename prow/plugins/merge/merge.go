/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package merge

import (
	"fmt"
	"regexp"

	"github.com/sirupsen/logrus"

	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/pluginhelp"
	"k8s.io/test-infra/prow/plugins"
	"k8s.io/test-infra/prow/repoowners"
)

const pluginName = "merge"

var (
	mergeLabel    = "ok-to-merge"
	mergeRe       = regexp.MustCompile(`(?mi)^/merge\s*$`)
	mergeCancelRe = regexp.MustCompile(`(?mi)^/merge cancel\s*$`)
)

func init() {
	plugins.RegisterGenericCommentHandler(pluginName, handleGenericCommentEvent, helpProvider)
}

func helpProvider(config *plugins.Configuration, enabledRepos []string) (*pluginhelp.PluginHelp, error) {
	// The Config field is omitted because this plugin is not configurable.
	pluginHelp := &pluginhelp.PluginHelp{
		Description: "The merge plugin manages the application and removal of the 'ok-to-merge' label which is typically used to gate merging.",
	}
	pluginHelp.AddCommand(pluginhelp.Command{
		Usage:       "/merge [cancel]",
		Description: "Adds or removes the 'ok-to-merge' label which is typically used to gate merging.",
		Featured:    true,
		WhoCanUse:   "Collaborators on the repository and the PR author.",
		Examples:    []string{"/merge", "/merge cancel"},
	})
	return pluginHelp, nil
}

// optionsForRepo gets the plugins.Merge struct that is applicable to the indicated repo.
func optionsForRepo(config *plugins.Configuration, org, repo string) *plugins.Merge {
	fullName := fmt.Sprintf("%s/%s", org, repo)
	for i := range config.Merge {
		if !strInSlice(org, config.Merge[i].Repos) && !strInSlice(fullName, config.Merge[i].Repos) {
			continue
		}
		return &config.Merge[i]
	}
	return &plugins.Merge{}
}
func strInSlice(str string, slice []string) bool {
	for _, elem := range slice {
		if elem == str {
			return true
		}
	}
	return false
}

type githubClient interface {
	IsCollaborator(owner, repo, login string) (bool, error)
	AddLabel(owner, repo string, number int, label string) error
	RemoveLabel(owner, repo string, number int, label string) error
	GetIssueLabels(org, repo string, number int) ([]github.Label, error)
	GetPullRequest(org, repo string, number int) (*github.PullRequest, error)
	ListIssueComments(org, repo string, number int) ([]github.IssueComment, error)
	DeleteComment(org, repo string, ID int) error
	BotName() (string, error)
}

// reviewCtx contains information about each review event
type reviewCtx struct {
	author, issueAuthor, body, htmlURL string
	repo                               github.Repo
	number                             int
}

func handleGenericCommentEvent(pc plugins.PluginClient, e github.GenericCommentEvent) error {
	return handleGenericComment(pc.GitHubClient, pc.PluginConfig, pc.OwnersClient, pc.Logger, e)
}

func handleGenericComment(gc githubClient, config *plugins.Configuration, ownersClient repoowners.Interface, log *logrus.Entry, e github.GenericCommentEvent) error {
	rc := reviewCtx{
		author:      e.User.Login,
		issueAuthor: e.IssueAuthor.Login,
		body:        e.Body,
		htmlURL:     e.HTMLURL,
		repo:        e.Repo,
		number:      e.Number,
	}

	// Only consider open PRs and new comments.
	if !e.IsPR || e.IssueState != "open" || e.Action != github.GenericCommentActionCreated {
		return nil
	}

	// If we create a "/merge" comment, add merge label if necessary.
	// If we create a "/merge cancel" comment, remove merge label if necessary.
	wantMerge := false
	if mergeRe.MatchString(rc.body) {
		wantMerge = true
	} else if mergeCancelRe.MatchString(rc.body) {
		wantMerge = false
	} else {
		return nil
	}

	return handle(wantMerge, config, ownersClient, rc, gc, log)
}

func handle(wantMerge bool, config *plugins.Configuration, ownersClient repoowners.Interface, rc reviewCtx, gc githubClient, log *logrus.Entry) error {
	//author := rc.author
	//issueAuthor := rc.issueAuthor
	number := rc.number
	//body := rc.body
	//htmlURL := rc.htmlURL
	org := rc.repo.Owner.Login
	repoName := rc.repo.Name

	// If we need to skip collaborator checks for this repo, what we actually need
	// to do is skip assignment checks and use OWNERS files to determine whether the
	// commenter can merge the PR.
	//skipCollaborators := skipCollaborators(config, org, repoName)
	//isAuthor := author == issueAuthor

	//TODO permission check

	// Only add the label if it doesn't have it, and vice versa.
	has := false
	labels, err := gc.GetIssueLabels(org, repoName, number)
	if err != nil {
		log.WithError(err).Errorf("Failed to get the labels on %s/%s#%d.", org, repoName, number)
	}

	has = github.HasLabel(mergeLabel, labels)

	if has && !wantMerge {
		log.Info("Removing merge label.")
		return gc.RemoveLabel(org, repoName, number, mergeLabel)
	} else if !has && wantMerge {
		log.Info("Adding merge label.")
		if err := gc.AddLabel(org, repoName, number, mergeLabel); err != nil {
			return err
		}
	}
	return nil
}

func skipCollaborators(config *plugins.Configuration, org, repo string) bool {
	full := fmt.Sprintf("%s/%s", org, repo)
	for _, elem := range config.Owners.SkipCollaborators {
		if elem == org || elem == full {
			return true
		}
	}
	return false
}
