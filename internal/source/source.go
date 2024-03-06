/*
Copyright 2024 The Flux authors

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

package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/fluxcd/pkg/git/repository"
	"github.com/fluxcd/pkg/runtime/acl"
	"github.com/go-git/go-git/v5/plumbing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
	"github.com/fluxcd/image-automation-controller/pkg/update"
)

var ErrInvalidSourceConfiguration = errors.New("invalid source configuration")

const defaultMessageTemplate = `Update from image update automation`

// TemplateData is the type of the value given to the commit message
// template.
type TemplateData struct {
	AutomationObject types.NamespacedName
	Updated          update.Result
	Changed          update.ResultV2
}

// SourceManager manages source.
type SourceManager struct {
	srcCfg           *gitSrcCfg
	automationObjKey types.NamespacedName
	gitClient        *gogit.Client
	workingDir       string
}

type SourceOptions struct {
	noCrossNamespaceRef    bool
	gitAllBranchReferences bool
}

type SourceOption func(*SourceOptions)

func WithSourceOptionNoCrossNamespaceRef() SourceOption {
	return func(so *SourceOptions) {
		so.noCrossNamespaceRef = true
	}
}

func WithSourceOptionGitAllBranchReferences() SourceOption {
	return func(so *SourceOptions) {
		so.gitAllBranchReferences = true
	}
}

// NewSourceManager takes all the provided inputs, validates them and returns a
// SourceManager which can be used to operate on the configured source.
func NewSourceManager(ctx context.Context, c client.Client, obj *imagev1.ImageUpdateAutomation, options ...SourceOption) (*SourceManager, error) {
	opts := &SourceOptions{}
	for _, o := range options {
		o(opts)
	}

	// Only GitRepository source is supported.
	if obj.Spec.SourceRef.Kind != sourcev1.GitRepositoryKind {
		return nil, fmt.Errorf("source kind '%s' not supported: %w", obj.Spec.SourceRef.Kind, ErrInvalidSourceConfiguration)
	}

	if obj.Spec.GitSpec == nil {
		return nil, fmt.Errorf("source kind '%s' necessitates field .spec.git: %w", sourcev1.GitRepositoryKind, ErrInvalidSourceConfiguration)
	}

	// Build source reference configuration to fetch and validate it.
	srcNamespace := obj.GetNamespace()
	if obj.Spec.SourceRef.Namespace != "" {
		srcNamespace = obj.Spec.SourceRef.Namespace
	}

	// srcKey is the GitRepository object key.
	srcKey := types.NamespacedName{Name: obj.Spec.SourceRef.Name, Namespace: srcNamespace}
	// originKey is the update automation object key.
	originKey := client.ObjectKeyFromObject(obj)

	// Check if the source is accessible.
	if opts.noCrossNamespaceRef && srcKey.Namespace != obj.GetNamespace() {
		return nil, acl.AccessDeniedError(fmt.Sprintf("can't access '%s/%s', cross-namespace references have been blocked", sourcev1.GitRepositoryKind, srcKey))
	}

	gitSrcCfg, err := buildGitConfig(ctx, c, originKey, srcKey, obj.Spec.GitSpec, *opts)
	if err != nil {
		return nil, err
	}

	workDir, err := os.MkdirTemp("", fmt.Sprintf("%s-%s", gitSrcCfg.srcKey.Namespace, gitSrcCfg.srcKey.Name))
	if err != nil {
		return nil, err
	}

	sm := &SourceManager{
		srcCfg:           gitSrcCfg,
		automationObjKey: originKey,
		workingDir:       workDir,
	}
	return sm, nil
}

// CreateWorkingDirectory creates a working directory for the SourceManager.
func (sm SourceManager) WorkDirectory() string {
	return sm.workingDir
}

// Cleanup deletes the working directory of the SourceManager.
func (sm SourceManager) Cleanup() error {
	return os.RemoveAll(sm.workingDir)
}

// SwitchBranch returns if the checkout branch and push branch are different.
func (sm SourceManager) SwitchBranch() bool {
	return sm.srcCfg.switchBranch
}

// CheckoutOption allows configuring the checkout options.
type CheckoutOption func(repository.CloneConfig)

// WithCheckoutOptionLastObserved is a CheckoutOption option to configure the
// last observed commit.
func WithCheckoutOptionLastObserved(commit string) CheckoutOption {
	return func(cc repository.CloneConfig) {
		cc.LastObservedCommit = commit
	}
}

// WithCheckoutOptionShallowClone is a CheckoutOption option to configure
// shallow clone.
func WithCheckoutOptionShallowClone() CheckoutOption {
	return func(cc repository.CloneConfig) {
		cc.ShallowClone = true
	}
}

func (sm *SourceManager) CheckoutSource(ctx context.Context, options ...CheckoutOption) (*git.Commit, error) {
	// Configuration clone options.
	cloneCfg := repository.CloneConfig{}
	if sm.srcCfg.checkoutRef != nil {
		cloneCfg.Tag = sm.srcCfg.checkoutRef.Tag
		cloneCfg.SemVer = sm.srcCfg.checkoutRef.SemVer
		cloneCfg.Commit = sm.srcCfg.checkoutRef.Commit
		cloneCfg.Branch = sm.srcCfg.checkoutRef.Branch
	}
	// Apply checkout configurations.
	for _, o := range options {
		o(cloneCfg)
	}

	var err error
	sm.gitClient, err = gogit.NewClient(sm.workingDir, sm.srcCfg.authOpts, sm.srcCfg.clientOpts...)
	if err != nil {
		return nil, err
	}

	commit, err := sm.gitClient.Clone(ctx, sm.srcCfg.url, cloneCfg)
	if err != nil {
		return nil, err
	}
	if sm.srcCfg.switchBranch {
		if err := sm.gitClient.SwitchBranch(ctx, sm.srcCfg.pushBranch); err != nil {
			return nil, err
		}
	}
	return commit, nil
}

type PushConfig func(repository.PushConfig)

func WithPushConfigForce() PushConfig {
	return func(pc repository.PushConfig) {
		pc.Force = true
	}
}

func WithPushConfigOptions(opts map[string]string) PushConfig {
	return func(pc repository.PushConfig) {
		pc.Options = opts
	}
}

func (sm SourceManager) CommitAndPush(ctx context.Context, obj *imagev1.ImageUpdateAutomation, policyResult update.ResultV2, pushOptions ...PushConfig) (*PushResult, error) {
	if len(policyResult.FileChanges) == 0 {
		return nil, nil
	}

	templateValues := &TemplateData{
		AutomationObject: sm.automationObjKey,
		Updated:          policyResult.ImageResult,
		Changed:          policyResult,
	}

	commitMsg, err := templateMsg(obj.Spec.GitSpec.Commit.MessageTemplate, templateValues)
	if err != nil {
		return nil, err
	}

	signature := git.Signature{
		Name:  obj.Spec.GitSpec.Commit.Author.Name,
		Email: obj.Spec.GitSpec.Commit.Author.Email,
		When:  time.Now(),
	}

	// The status message depends on what happens next. Since there's
	// more than one way to succeed, there's some if..else below, and
	// early returns only on failure.

	var rev string
	var commitErr error
	rev, commitErr = sm.gitClient.Commit(
		git.Commit{
			Author:  signature,
			Message: commitMsg,
		},
		repository.WithSigner(sm.srcCfg.signingEntity),
	)

	// TODO: Get code for force push and status message.
	if commitErr != nil {
		if !errors.Is(commitErr, git.ErrNoStagedFiles) {
			return nil, commitErr
		}

		// TODO: log no changes made, no commit. Maybe return a special error as
		// this didn't result in a push or commit?

		return nil, nil
	}

	pushConfig := repository.PushConfig{}
	for _, po := range pushOptions {
		po(pushConfig)
	}

	// TODO: Build a new context for push operation.

	// TODO: As per the docs and old code, push branch will always be present.
	// If not, the automation will fail. Verify.
	if err := sm.gitClient.Push(ctx, pushConfig); err != nil {
		return nil, err
	}
	// TODO: Log about the pushed commit and construct status message.

	if obj.Spec.GitSpec.HasRefspec() {
		pushConfig.Refspecs = append(pushConfig.Refspecs, obj.Spec.GitSpec.Push.Refspec)
		if err := sm.gitClient.Push(ctx, pushConfig); err != nil {
			return nil, err
		}
		// TODO: Log about the pushed commit.
	}

	return NewPushResult(sm.srcCfg.pushBranch, rev, commitMsg, sm.srcCfg.switchBranch)
}

// templateMsg renders a msg template, returning the message or an error.
func templateMsg(messageTemplate string, templateValues *TemplateData) (string, error) {
	if messageTemplate == "" {
		messageTemplate = defaultMessageTemplate
	}

	// Includes only functions that are guaranteed to always evaluate to the same result for given input.
	// This removes the possibility of accidentally relying on where or when the template runs.
	// https://github.com/Masterminds/sprig/blob/3ac42c7bc5e4be6aa534e036fb19dde4a996da2e/functions.go#L70
	t, err := template.New("commit message").Funcs(sprig.HermeticTxtFuncMap()).Parse(messageTemplate)
	if err != nil {
		return "", fmt.Errorf("unable to create commit message template from spec: %w", err)
	}

	b := &strings.Builder{}
	if err := t.Execute(b, *templateValues); err != nil {
		return "", fmt.Errorf("failed to run template from spec: %w", err)
	}
	return b.String(), nil
}

// PushResult is the result of a push operation.
type PushResult struct {
	commit       *git.Commit
	switchBranch bool
	creationTime *metav1.Time
}

// NewPushResult returns a new PushResult.
func NewPushResult(branch string, rev string, commitMsg string, switchBranch bool) (*PushResult, error) {
	if rev == "" {
		return nil, errors.New("empty push commit revision")
	}

	return &PushResult{
		commit: &git.Commit{
			Hash:      git.ExtractHashFromRevision(rev),
			Reference: plumbing.NewBranchReferenceName(branch).String(),
			Message:   commitMsg,
		},
		switchBranch: switchBranch,
		creationTime: &metav1.Time{Time: time.Now()},
	}, nil
}

// Commit returns the revision of the pushed commit.
func (pr PushResult) Commit() *git.Commit {
	return pr.commit
}

// Time returns the time at which the push was performed.
func (pr PushResult) Time() *metav1.Time {
	return pr.creationTime
}

// SwitchBranch returns if the source has different checkout and push branch.
func (pr PushResult) SwitchBranch() bool {
	return pr.switchBranch
}
