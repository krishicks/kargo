package builtin

import (
	"context"
	"errors"
	"time"

	"k8s.io/utils/ptr"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	argocd "github.com/akuity/kargo/pkg/controller/argocd/api/v1alpha1"
	"github.com/akuity/kargo/pkg/logging"
	"github.com/akuity/kargo/pkg/promotion"
	"github.com/akuity/kargo/pkg/urls"
	"github.com/akuity/kargo/pkg/x/promotion/runner/builtin"
)

const stepKindArgoCDAwait = "argocd-await"

func init() {
	promotion.DefaultStepRunnerRegistry.MustRegister(
		promotion.StepRunnerRegistration{
			Name: stepKindArgoCDAwait,
			Metadata: promotion.StepRunnerMetadata{
				DefaultTimeout: 5 * time.Minute,
				RequiredCapabilities: []promotion.StepRunnerCapability{
					promotion.StepCapabilityAccessArgoCD,
				},
			},
			Value: newArgocdAwaiter,
		},
	)
}

// argocdAwaiter is an implementation of the promotion.StepRunner interface that
// waits for one or more Argo CD Application resources to reach desired
// revisions without triggering a sync.
type argocdAwaiter struct {
	argocdBase
}

// newArgocdAwaiter returns an implementation of the promotion.StepRunner
// interface that waits for Argo CD Application resources to reach desired
// revisions.
func newArgocdAwaiter(
	caps promotion.StepRunnerCapabilities,
) promotion.StepRunner {
	a := &argocdAwaiter{}
	a.argocdClient = caps.ArgoCDClient
	a.getAuthorizedApplicationsFn = a.getAuthorizedApplications
	a.buildLabelSelectorFn = buildArgoCDLabelSelector
	a.schemaLoader = getConfigSchemaLoader(stepKindArgoCDAwait)
	return a
}

// Run implements the promotion.StepRunner interface.
func (a *argocdAwaiter) Run(
	ctx context.Context,
	stepCtx *promotion.StepContext,
) (promotion.StepResult, error) {
	cfg, err := a.convert(stepCtx.Config)
	if err != nil {
		return promotion.StepResult{
			Status: kargoapi.PromotionStepStatusFailed,
		}, &promotion.TerminalError{Err: err}
	}
	return a.run(ctx, stepCtx, cfg)
}

// convert validates argocdAwaiter configuration against a JSON schema and
// converts it into a builtin.ArgoCDAwaitConfig struct.
func (a *argocdAwaiter) convert(
	cfg promotion.Config,
) (builtin.ArgoCDAwaitConfig, error) {
	return validateAndConvert[builtin.ArgoCDAwaitConfig](
		a.schemaLoader, cfg, stepKindArgoCDAwait,
	)
}

func (a *argocdAwaiter) run(
	ctx context.Context,
	stepCtx *promotion.StepContext,
	stepCfg builtin.ArgoCDAwaitConfig,
) (promotion.StepResult, error) {
	if a.argocdClient == nil {
		// nolint:staticcheck
		return promotion.StepResult{Status: kargoapi.PromotionStepStatusErrored}, errors.New(
			"Argo CD integration is disabled on this controller; cannot " +
				"check Argo CD Application resources",
		)
	}

	logger := logging.LoggerFromContext(ctx)
	logger.Info("executing argocd-await promotion step")

	allMatch := true
	for i := range stepCfg.Apps {
		appCfg := &stepCfg.Apps[i]

		apps, err := a.getAuthorizedApplicationsFn(ctx, stepCtx, appCfg)
		if err != nil {
			return promotion.StepResult{
				Status: kargoapi.PromotionStepStatusErrored,
			}, err
		}

		if appCfg.Selector != nil && len(apps) > 0 {
			logger.Info(
				"found Applications matching selector",
				"count", len(apps),
				"namespace", appCfg.Namespace,
			)
		}

		for _, app := range apps {
			desiredRevisions := a.getDesiredRevisions(appCfg, app)
			if !a.checkRevisions(app, desiredRevisions) {
				logger.Info(
					"Application has not yet reached desired revisions",
					"app", app.Name,
					"namespace", app.Namespace,
				)
				allMatch = false
			} else {
				logger.Info(
					"Application has reached desired revisions",
					"app", app.Name,
					"namespace", app.Namespace,
				)
			}
		}
	}

	if allMatch {
		logger.Info("all Applications have reached desired revisions")
		return promotion.StepResult{
			Status: kargoapi.PromotionStepStatusSucceeded,
		}, nil
	}

	logger.Info("waiting for Applications to reach desired revisions")
	return promotion.StepResult{
		Status:     kargoapi.PromotionStepStatusRunning,
		RetryAfter: ptr.To(30 * time.Second),
	}, nil
}

// getDesiredRevisions returns the desired revisions for all sources of the
// given Application, matched by repoURL (and optionally chart) from the
// await config.
func (a *argocdAwaiter) getDesiredRevisions(
	appCfg *builtin.ArgoCDAwaitApp,
	app *argocd.Application,
) []string {
	if app == nil {
		return nil
	}
	sources := app.Spec.Sources
	if len(sources) == 0 && app.Spec.Source != nil {
		sources = []argocd.ApplicationSource{*app.Spec.Source}
	}
	if len(sources) == 0 {
		return nil
	}
	revisions := make([]string, len(sources))
	for i, src := range sources {
		if srcCfg := a.findSourceConfig(appCfg, src); srcCfg != nil {
			revisions[i] = srcCfg.DesiredRevision
		}
	}
	return revisions
}

// findSourceConfig finds the ArgoCDAwaitSource that matches the given
// Application source by repoURL and chart.
func (a *argocdAwaiter) findSourceConfig(
	appCfg *builtin.ArgoCDAwaitApp,
	src argocd.ApplicationSource,
) *builtin.ArgoCDAwaitSource {
	for i := range appCfg.Sources {
		srcCfg := &appCfg.Sources[i]
		if src.Chart != "" || srcCfg.Chart != "" {
			if src.RepoURL == srcCfg.RepoURL &&
				src.Chart == srcCfg.Chart {
				return srcCfg
			}
		} else {
			if urls.NormalizeGit(src.RepoURL) ==
				urls.NormalizeGit(srcCfg.RepoURL) {
				return srcCfg
			}
		}
	}
	return nil
}

// checkRevisions compares the Application's observed sync revisions against
// the desired revisions. Returns true only if the Application has no active
// sync operation and all non-empty desired revisions match. The last completed
// sync operation's SyncResult is checked first; if it matches, the app is
// considered synced even if the sync status has moved ahead. If the SyncResult
// does not match (or is absent), the sync status revisions are checked instead,
// but only when the app is Synced.
func (a *argocdAwaiter) checkRevisions(
	app *argocd.Application,
	desiredRevisions []string,
) bool {
	// If there is a pending or in-progress sync operation, we must wait for it
	// to complete before evaluating the sync status and revisions.
	if app.Operation != nil {
		return false
	}
	if app.Status.OperationState != nil &&
		!app.Status.OperationState.Phase.Completed() {
		return false
	}

	// Check the last sync operation's result revisions first when available.
	// If they match, the app is considered synced even if the sync status has
	// moved ahead (e.g. new commits that don't affect this app).
	if app.Status.OperationState != nil &&
		app.Status.OperationState.SyncResult != nil {
		if a.revisionsMatch(
			ensureSlice(
				app.Status.OperationState.SyncResult.Revisions,
				app.Status.OperationState.SyncResult.Revision,
			),
			desiredRevisions,
		) {
			return true
		}
	}

	// Fall back to the sync status revisions, but only if the app is Synced.
	if app.Status.Sync.Status != argocd.SyncStatusCodeSynced {
		return false
	}

	return a.revisionsMatch(
		ensureSlice(
			app.Status.Sync.Revisions,
			app.Status.Sync.Revision,
		),
		desiredRevisions,
	)
}

// revisionsMatch returns true if all non-empty desired revisions match the
// corresponding observed revisions.
func (a *argocdAwaiter) revisionsMatch(
	observedRevisions []string,
	desiredRevisions []string,
) bool {
	for i, desired := range desiredRevisions {
		if desired == "" {
			continue
		}
		if i >= len(observedRevisions) ||
			observedRevisions[i] != desired {
			return false
		}
	}
	return true
}

// ensureSlice returns slice if non-empty, otherwise wraps fallback in a
// single-element slice.
func ensureSlice[T any](slice []T, fallback T) []T {
	if len(slice) > 0 {
		return slice
	}
	return []T{fallback}
}
