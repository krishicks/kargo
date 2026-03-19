package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	argocd "github.com/akuity/kargo/pkg/controller/argocd/api/v1alpha1"
	"github.com/akuity/kargo/pkg/promotion"
	"github.com/akuity/kargo/pkg/x/promotion/runner/builtin"
)

func Test_authorizeArgoCDAppAccess(t *testing.T) {
	const (
		permErr           = "does not permit access"
		parseErr          = "unable to parse"
		deprecatedGlobErr = "deprecated glob expression"
	)

	testCases := []struct {
		name    string
		stepCtx *promotion.StepContext
		appMeta metav1.ObjectMeta
		errMsg  string
	}{
		{
			name: "no annotation",
			stepCtx: &promotion.StepContext{
				Project: "my-project",
				Stage:   "my-stage",
			},
			appMeta: metav1.ObjectMeta{
				Name:      "my-app",
				Namespace: "argocd",
			},
			errMsg: permErr,
		},
		{
			name: "annotations are nil",
			stepCtx: &promotion.StepContext{
				Project: "ns-yep",
				Stage:   "name-yep",
			},
			appMeta: metav1.ObjectMeta{},
			errMsg:  permErr,
		},
		{
			name: "annotation is missing",
			stepCtx: &promotion.StepContext{
				Project: "ns-yep",
				Stage:   "name-yep",
			},
			appMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
			errMsg: permErr,
		},
		{
			name: "authorized",
			stepCtx: &promotion.StepContext{
				Project: "my-project",
				Stage:   "my-stage",
			},
			appMeta: metav1.ObjectMeta{
				Name:      "my-app",
				Namespace: "argocd",
				Annotations: map[string]string{
					kargoapi.AnnotationKeyAuthorizedStage: "my-project:my-stage",
				},
			},
		},
		{
			name: "wrong project",
			stepCtx: &promotion.StepContext{
				Project: "my-project",
				Stage:   "my-stage",
			},
			appMeta: metav1.ObjectMeta{
				Name:      "my-app",
				Namespace: "argocd",
				Annotations: map[string]string{
					kargoapi.AnnotationKeyAuthorizedStage: "other-project:my-stage",
				},
			},
			errMsg: permErr,
		},
		{
			name: "wrong stage",
			stepCtx: &promotion.StepContext{
				Project: "ns-yep",
				Stage:   "name-yep",
			},
			appMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					kargoapi.AnnotationKeyAuthorizedStage: "ns-nope:name-nope",
				},
			},
			errMsg: permErr,
		},
		{
			name: "annotation cannot be parsed",
			stepCtx: &promotion.StepContext{
				Project: "ns-yep",
				Stage:   "name-yep",
			},
			appMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					kargoapi.AnnotationKeyAuthorizedStage: "bogus",
				},
			},
			errMsg: parseErr,
		},
		{
			name: "glob expression not allowed",
			stepCtx: &promotion.StepContext{
				Project: "my-project",
				Stage:   "my-stage",
			},
			appMeta: metav1.ObjectMeta{
				Name:      "my-app",
				Namespace: "argocd",
				Annotations: map[string]string{
					kargoapi.AnnotationKeyAuthorizedStage: "my-project:*",
				},
			},
			errMsg: deprecatedGlobErr,
		},
		{
			name: "wildcard namespace with full name",
			stepCtx: &promotion.StepContext{
				Project: "ns-yep",
				Stage:   "name-yep",
			},
			appMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					kargoapi.AnnotationKeyAuthorizedStage: "*:name-yep",
				},
			},
			errMsg: deprecatedGlobErr,
		},
		{
			name: "malformed annotation",
			stepCtx: &promotion.StepContext{
				Project: "my-project",
				Stage:   "my-stage",
			},
			appMeta: metav1.ObjectMeta{
				Name:      "my-app",
				Namespace: "argocd",
				Annotations: map[string]string{
					kargoapi.AnnotationKeyAuthorizedStage: "no-colon",
				},
			},
			errMsg: parseErr,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := authorizeArgoCDAppAccess(tc.stepCtx, tc.appMeta)
			if tc.errMsg == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.errMsg)
			}
		})
	}
}

func Test_buildArgoCDLabelSelector(t *testing.T) {
	testCases := []struct {
		name     string
		selector *builtin.ArgoCDAppSelector
		assert   func(*testing.T, labels.Selector, error)
	}{
		{
			name:     "empty selector",
			selector: &builtin.ArgoCDAppSelector{},
			assert: func(t *testing.T, _ labels.Selector, err error) {
				require.Error(t, err)
				assert.Contains(
					t, err.Error(),
					"at least one match criterion",
				)
			},
		},
		{
			name: "empty maps return error",
			selector: &builtin.ArgoCDAppSelector{
				MatchLabels:      map[string]string{},
				MatchExpressions: []builtin.MatchExpression{},
			},
			assert: func(t *testing.T, sel labels.Selector, err error) {
				require.ErrorContains(
					t, err,
					"selector must have at least one match criterion",
				)
				require.Nil(t, sel)
			},
		},
		{
			name: "valid matchLabels",
			selector: &builtin.ArgoCDAppSelector{
				MatchLabels: map[string]string{
					"env": "prod",
				},
			},
			assert: func(t *testing.T, sel labels.Selector, err error) {
				require.NoError(t, err)
				require.NotNil(t, sel)
			},
		},
		{
			name: "matchLabels matching behavior",
			selector: &builtin.ArgoCDAppSelector{
				MatchLabels: map[string]string{
					"env":  "prod",
					"team": "platform",
				},
			},
			assert: func(t *testing.T, sel labels.Selector, err error) {
				require.NoError(t, err)
				require.NotNil(t, sel)
				assert.True(t, sel.Matches(labels.Set{
					"env": "prod", "team": "platform",
				}))
				assert.False(t, sel.Matches(labels.Set{
					"env": "dev", "team": "platform",
				}))
			},
		},
		{
			name: "valid matchExpressions",
			selector: &builtin.ArgoCDAppSelector{
				MatchExpressions: []builtin.MatchExpression{{
					Key:      "env",
					Operator: builtin.In,
					Values:   []string{"prod", "staging"},
				}},
			},
			assert: func(t *testing.T, sel labels.Selector, err error) {
				require.NoError(t, err)
				require.NotNil(t, sel)
				assert.True(t, sel.Matches(labels.Set{"env": "prod"}))
				assert.True(t, sel.Matches(labels.Set{
					"env": "staging",
				}))
				assert.False(t, sel.Matches(labels.Set{
					"env": "dev",
				}))
			},
		},
		{
			name: "both matchLabels and matchExpressions",
			selector: &builtin.ArgoCDAppSelector{
				MatchLabels: map[string]string{
					"team": "platform",
				},
				MatchExpressions: []builtin.MatchExpression{{
					Key:      "env",
					Operator: builtin.In,
					Values:   []string{"prod", "staging"},
				}},
			},
			assert: func(t *testing.T, sel labels.Selector, err error) {
				require.NoError(t, err)
				require.NotNil(t, sel)
				assert.True(t, sel.Matches(labels.Set{
					"env": "prod", "team": "platform",
				}))
				assert.False(t, sel.Matches(labels.Set{
					"env": "prod", "team": "other",
				}))
				assert.False(t, sel.Matches(labels.Set{
					"env": "dev", "team": "platform",
				}))
			},
		},
		{
			name: "NotIn operator",
			selector: &builtin.ArgoCDAppSelector{
				MatchExpressions: []builtin.MatchExpression{{
					Key:      "env",
					Operator: builtin.NotIn,
					Values:   []string{"dev", "test"},
				}},
			},
			assert: func(t *testing.T, sel labels.Selector, err error) {
				require.NoError(t, err)
				require.NotNil(t, sel)
				assert.True(t, sel.Matches(labels.Set{"env": "prod"}))
				assert.False(t, sel.Matches(labels.Set{"env": "dev"}))
			},
		},
		{
			name: "Exists operator",
			selector: &builtin.ArgoCDAppSelector{
				MatchExpressions: []builtin.MatchExpression{{
					Key:      "environment",
					Operator: builtin.Exists,
				}},
			},
			assert: func(t *testing.T, sel labels.Selector, err error) {
				require.NoError(t, err)
				require.NotNil(t, sel)
				assert.True(t, sel.Matches(labels.Set{
					"environment": "prod",
				}))
				assert.False(t, sel.Matches(labels.Set{
					"env": "prod",
				}))
			},
		},
		{
			name: "DoesNotExist operator",
			selector: &builtin.ArgoCDAppSelector{
				MatchExpressions: []builtin.MatchExpression{{
					Key:      "deprecated",
					Operator: builtin.DoesNotExist,
				}},
			},
			assert: func(t *testing.T, sel labels.Selector, err error) {
				require.NoError(t, err)
				require.NotNil(t, sel)
				assert.True(t, sel.Matches(labels.Set{"env": "prod"}))
				assert.False(t, sel.Matches(labels.Set{
					"deprecated": "true",
				}))
			},
		},
		{
			name: "invalid operator",
			selector: &builtin.ArgoCDAppSelector{
				MatchExpressions: []builtin.MatchExpression{{
					Key:      "env",
					Operator: "BadOp",
				}},
			},
			assert: func(t *testing.T, _ labels.Selector, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid operator")
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sel, err := buildArgoCDLabelSelector(tc.selector)
			tc.assert(t, sel, err)
		})
	}
}

func Test_argocdBase_getAuthorizedApplications(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, argocd.AddToScheme(scheme))

	testCases := []struct {
		name        string
		apps        []*argocd.Application
		appRef      builtin.ArgoCDAppReference
		interceptor interceptor.Funcs
		assert      func(*testing.T, []*argocd.Application, error)
	}{
		{
			name: "selector returns multiple authorized apps",
			apps: []*argocd.Application{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app1",
						Namespace: "argocd",
						Labels:    map[string]string{"env": "prod"},
						Annotations: map[string]string{
							kargoapi.AnnotationKeyAuthorizedStage: "fake-project:fake-stage",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app2",
						Namespace: "argocd",
						Labels:    map[string]string{"env": "prod"},
						Annotations: map[string]string{
							kargoapi.AnnotationKeyAuthorizedStage: "fake-project:fake-stage",
						},
					},
				},
			},
			appRef: &builtin.ArgoCDAppUpdate{
				Namespace: "argocd",
				Selector: &builtin.ArgoCDAppSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.NoError(t, err)
				assert.Len(t, apps, 2)
				assert.Equal(t, "app1", apps[0].Name)
				assert.Equal(t, "app2", apps[1].Name)
			},
		},
		{
			name: "selector filters out unauthorized apps",
			apps: []*argocd.Application{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app1",
						Namespace: "argocd",
						Labels:    map[string]string{"env": "prod"},
						Annotations: map[string]string{
							kargoapi.AnnotationKeyAuthorizedStage: "fake-project:fake-stage",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app2",
						Namespace: "argocd",
						Labels:    map[string]string{"env": "prod"},
					},
				},
			},
			appRef: &builtin.ArgoCDAppUpdate{
				Namespace: "argocd",
				Selector: &builtin.ArgoCDAppSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.NoError(t, err)
				assert.Len(t, apps, 1)
				assert.Equal(t, "app1", apps[0].Name)
			},
		},
		{
			name: "selector returns no apps",
			apps: []*argocd.Application{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app1",
						Namespace: "argocd",
						Labels:    map[string]string{"env": "dev"},
					},
				},
			},
			appRef: &builtin.ArgoCDAppUpdate{
				Namespace: "argocd",
				Selector: &builtin.ArgoCDAppSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.ErrorContains(
					t, err,
					"no Argo CD Applications found matching selector",
				)
				require.Nil(t, apps)
			},
		},
		{
			name: "selector matches apps but none authorized",
			apps: []*argocd.Application{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app1",
						Namespace: "argocd",
						Labels:    map[string]string{"env": "prod"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app2",
						Namespace: "argocd",
						Labels:    map[string]string{"env": "prod"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app3",
						Namespace: "argocd",
						Labels:    map[string]string{"env": "prod"},
					},
				},
			},
			appRef: &builtin.ArgoCDAppUpdate{
				Namespace: "argocd",
				Selector: &builtin.ArgoCDAppSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.ErrorContains(
					t, err,
					"found 3 Application(s) matching selector",
				)
				require.ErrorContains(
					t, err,
					"but none are authorized for Stage "+
						"fake-project:fake-stage",
				)
				require.Nil(t, apps)
			},
		},
		{
			name: "name-based selection returns single app",
			apps: []*argocd.Application{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-app",
						Namespace: "argocd",
						Annotations: map[string]string{
							kargoapi.AnnotationKeyAuthorizedStage: "fake-project:fake-stage",
						},
					},
				},
			},
			appRef: &builtin.ArgoCDAppUpdate{
				Name:      "my-app",
				Namespace: "argocd",
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.NoError(t, err)
				require.Len(t, apps, 1)
				require.Equal(t, "my-app", apps[0].Name)
			},
		},
		{
			name: "name-based selection app not found",
			apps: []*argocd.Application{},
			appRef: &builtin.ArgoCDAppUpdate{
				Name:      "nonexistent-app",
				Namespace: "argocd",
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.ErrorContains(
					t, err,
					"unable to find Argo CD Application",
				)
				require.Nil(t, apps)
			},
		},
		{
			name: "name-based selection app not authorized",
			apps: []*argocd.Application{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-app",
						Namespace: "argocd",
					},
				},
			},
			appRef: &builtin.ArgoCDAppUpdate{
				Name:      "my-app",
				Namespace: "argocd",
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.ErrorContains(t, err, "is not authorized")
				require.Nil(t, apps)
			},
		},
		{
			name: "error listing applications",
			appRef: &builtin.ArgoCDAppUpdate{
				Namespace: "argocd",
				Selector: &builtin.ArgoCDAppSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
			},
			interceptor: interceptor.Funcs{
				List: func(
					context.Context,
					client.WithWatch,
					client.ObjectList,
					...client.ListOption,
				) error {
					return errors.New("something went wrong")
				},
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.ErrorContains(
					t, err,
					"error listing Argo CD Applications",
				)
				require.ErrorContains(
					t, err, "something went wrong",
				)
				require.Nil(t, apps)
			},
		},
		{
			name: "works with ArgoCDAwaitApp",
			apps: []*argocd.Application{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-app",
						Namespace: "argocd",
						Annotations: map[string]string{
							kargoapi.AnnotationKeyAuthorizedStage: "fake-project:fake-stage",
						},
					},
				},
			},
			appRef: &builtin.ArgoCDAwaitApp{
				Name:      "my-app",
				Namespace: "argocd",
			},
			assert: func(
				t *testing.T,
				apps []*argocd.Application,
				err error,
			) {
				require.NoError(t, err)
				require.Len(t, apps, 1)
				require.Equal(t, "my-app", apps[0].Name)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(tc.interceptor)

			if len(tc.apps) > 0 {
				objects := make([]client.Object, len(tc.apps))
				for i, app := range tc.apps {
					objects[i] = app
				}
				c.WithObjects(objects...)
			}

			b := &argocdBase{
				argocdClient: c.Build(),
			}
			b.getAuthorizedApplicationsFn = b.getAuthorizedApplications
			b.buildLabelSelectorFn = buildArgoCDLabelSelector

			apps, err := b.getAuthorizedApplications(
				context.Background(),
				&promotion.StepContext{
					Project: "fake-project",
					Stage:   "fake-stage",
				},
				tc.appRef,
			)
			tc.assert(t, apps, err)
		})
	}
}
