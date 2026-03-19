package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	libargocd "github.com/akuity/kargo/pkg/argocd"
	argocd "github.com/akuity/kargo/pkg/controller/argocd/api/v1alpha1"
	"github.com/akuity/kargo/pkg/logging"
	"github.com/akuity/kargo/pkg/promotion"
	"github.com/akuity/kargo/pkg/x/promotion/runner/builtin"
)

// argocdBase holds shared fields and behavior for interacting with Argo CD
// Application resources.
type argocdBase struct {
	schemaLoader gojsonschema.JSONLoader
	argocdClient client.Client

	// These behaviors are overridable for testing purposes:

	getAuthorizedApplicationsFn func(
		context.Context,
		*promotion.StepContext,
		builtin.ArgoCDAppReference,
	) ([]*argocd.Application, error)

	buildLabelSelectorFn func(
		*builtin.ArgoCDAppSelector,
	) (labels.Selector, error)
}

// getAuthorizedApplications returns a slice of Argo CD Applications that match
// the given app reference (either by name or by label selector) and are
// authorized for access by the Kargo Stage.
func (b *argocdBase) getAuthorizedApplications(
	ctx context.Context,
	stepCtx *promotion.StepContext,
	appRef builtin.ArgoCDAppReference,
) ([]*argocd.Application, error) {
	namespace := appRef.GetNamespace()
	if namespace == "" {
		namespace = libargocd.Namespace()
	}

	var apps []*argocd.Application

	if appRef.GetSelector() != nil {
		labelSelector, err :=
			b.buildLabelSelectorFn(appRef.GetSelector())
		if err != nil {
			return nil, fmt.Errorf(
				"error building label selector: %w", err,
			)
		}

		appList := &argocd.ApplicationList{}
		listOpts := []client.ListOption{
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{
				Selector: labelSelector,
			},
		}
		if err = b.argocdClient.List(
			ctx, appList, listOpts...,
		); err != nil {
			return nil, fmt.Errorf(
				"error listing Argo CD Applications matching "+
					"selector: %w",
				err,
			)
		}
		for i := range appList.Items {
			apps = append(apps, &appList.Items[i])
		}
	} else {
		app, err := argocd.GetApplication(
			ctx,
			b.argocdClient,
			namespace,
			appRef.GetName(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"error finding Argo CD Application %q in "+
					"namespace %q: %w",
				appRef.GetName(), namespace, err,
			)
		}
		if app == nil {
			return nil, fmt.Errorf(
				"unable to find Argo CD Application %q in "+
					"namespace %q",
				appRef.GetName(), namespace,
			)
		}
		apps = append(apps, app)
	}

	logger := logging.LoggerFromContext(ctx)
	authorizedApps := make([]*argocd.Application, 0, len(apps))
	for _, app := range apps {
		if err := authorizeArgoCDAppAccess(
			stepCtx, app.ObjectMeta,
		); err != nil {
			logger.Info(
				"skipping unauthorized Application",
				"app", app.Name,
				"namespace", app.Namespace,
				"reason", err.Error(),
			)
			continue
		}
		authorizedApps = append(authorizedApps, app)
	}

	if len(authorizedApps) == 0 {
		if appRef.GetSelector() != nil {
			totalAppsFound := len(apps)
			if totalAppsFound == 0 {
				return nil, fmt.Errorf(
					"no Argo CD Applications found matching "+
						"selector in namespace %q",
					namespace,
				)
			}
			return nil, fmt.Errorf(
				"found %d Application(s) matching selector in "+
					"namespace %q, but none are authorized for "+
					"Stage %s:%s",
				totalAppsFound, namespace,
				stepCtx.Project, stepCtx.Stage,
			)
		}
		// nolint:staticcheck
		return nil, fmt.Errorf(
			"Argo CD Application %q in namespace %q is not "+
				"authorized",
			appRef.GetName(), namespace,
		)
	}

	return authorizedApps, nil
}

// authorizeArgoCDAppAccess returns an error if the Argo CD Application
// represented by appMeta does not explicitly permit access by the Kargo Stage.
func authorizeArgoCDAppAccess(
	stepCtx *promotion.StepContext,
	appMeta metav1.ObjectMeta,
) error {
	// nolint:staticcheck
	permErr := fmt.Errorf(
		"Argo CD Application %q in namespace %q does not permit "+
			"access by Kargo Stage %s in namespace %s",
		appMeta.Name,
		appMeta.Namespace,
		stepCtx.Stage,
		stepCtx.Project,
	)

	allowedStage, ok :=
		appMeta.Annotations[kargoapi.AnnotationKeyAuthorizedStage]
	if !ok {
		return permErr
	}

	tokens := strings.SplitN(allowedStage, ":", 2)
	if len(tokens) != 2 {
		return fmt.Errorf(
			"unable to parse value of annotation %q (%q) on "+
				"Argo CD Application %q in namespace %q",
			kargoapi.AnnotationKeyAuthorizedStage,
			allowedStage,
			appMeta.Name,
			appMeta.Namespace,
		)
	}

	projectName, stageName := tokens[0], tokens[1]
	if strings.Contains(projectName, "*") ||
		strings.Contains(stageName, "*") {
		// nolint:staticcheck
		return fmt.Errorf(
			"Argo CD Application %q in namespace %q has "+
				"deprecated glob expression in annotation %q (%q)",
			appMeta.Name,
			appMeta.Namespace,
			kargoapi.AnnotationKeyAuthorizedStage,
			allowedStage,
		)
	}
	if projectName != stepCtx.Project ||
		stageName != stepCtx.Stage {
		return permErr
	}
	return nil
}

// buildArgoCDLabelSelector converts an ArgoCDAppSelector into a Kubernetes
// labels.Selector.
func buildArgoCDLabelSelector(
	selector *builtin.ArgoCDAppSelector,
) (labels.Selector, error) {
	if len(selector.MatchLabels) == 0 &&
		len(selector.MatchExpressions) == 0 {
		return nil, fmt.Errorf(
			"selector must have at least one match criterion",
		)
	}

	labelSelector := labels.NewSelector()

	for key, value := range selector.MatchLabels {
		req, err := labels.NewRequirement(
			key, selection.Equals, []string{value},
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid matchLabel %s=%s: %w", key, value, err,
			)
		}
		labelSelector = labelSelector.Add(*req)
	}

	for _, expr := range selector.MatchExpressions {
		var op selection.Operator
		switch expr.Operator {
		case builtin.In:
			op = selection.In
		case builtin.NotIn:
			op = selection.NotIn
		case builtin.Exists:
			op = selection.Exists
		case builtin.DoesNotExist:
			op = selection.DoesNotExist
		default:
			return nil, fmt.Errorf(
				"invalid operator: %s", expr.Operator,
			)
		}

		req, err := labels.NewRequirement(
			expr.Key, op, expr.Values,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid matchExpression: %w", err,
			)
		}
		labelSelector = labelSelector.Add(*req)
	}

	return labelSelector, nil
}
