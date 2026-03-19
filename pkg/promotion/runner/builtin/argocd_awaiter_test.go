package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kargoapi "github.com/akuity/kargo/api/v1alpha1"
	argocd "github.com/akuity/kargo/pkg/controller/argocd/api/v1alpha1"
	"github.com/akuity/kargo/pkg/promotion"
	"github.com/akuity/kargo/pkg/x/promotion/runner/builtin"
)

func Test_newArgocdAwaiter(t *testing.T) {
	r := newArgocdAwaiter(promotion.StepRunnerCapabilities{
		ArgoCDClient: fake.NewFakeClient(),
	})
	runner, ok := r.(*argocdAwaiter)
	require.True(t, ok)
	assert.NotNil(t, runner.argocdClient)
	assert.NotNil(t, runner.schemaLoader)
	assert.NotNil(t, runner.getAuthorizedApplicationsFn)
	assert.NotNil(t, runner.buildLabelSelectorFn)
}

func Test_argocdAwaiter_convert(t *testing.T) {
	tests := []validationTestCase{
		{
			name:   "apps not specified",
			config: promotion.Config{},
			expectedProblems: []string{
				"(root): apps is required",
			},
		},
		{
			name: "apps is empty array",
			config: promotion.Config{
				"apps": []promotion.Config{},
			},
			expectedProblems: []string{
				"apps: Array must have at least 1 items",
			},
		},
		{
			name: "app name and selector not specified",
			config: promotion.Config{
				"apps": []promotion.Config{{}},
			},
			expectedProblems: []string{
				"apps.0: Must validate one and only one schema (oneOf)",
			},
		},
		{
			name: "app name is empty string",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "",
				}},
			},
			expectedProblems: []string{
				"apps.0.name: String length must be greater than or equal to 1",
			},
		},
		{
			name: "app name and selector both specified",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "my-app",
					"selector": promotion.Config{
						"matchLabels": promotion.Config{
							"env": "prod",
						},
					},
				}},
			},
			expectedProblems: []string{
				"apps.0: Must validate one and only one schema (oneOf)",
			},
		},
		{
			name: "app selector is empty",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"selector": promotion.Config{},
				}},
			},
			expectedProblems: []string{
				"apps.0.selector: Must validate at least one schema (anyOf)",
			},
		},
		{
			name: "app selector matchLabels is empty object",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"selector": promotion.Config{
						"matchLabels": promotion.Config{},
					},
				}},
			},
			expectedProblems: []string{
				"apps.0.selector.matchLabels: Must have at least 1 properties",
			},
		},
		{
			name: "app selector matchExpressions is empty array",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"selector": promotion.Config{
						"matchExpressions": []promotion.Config{},
					},
				}},
			},
			expectedProblems: []string{
				"apps.0.selector.matchExpressions: Array must have at least 1 items",
			},
		},
		{
			name: "app namespace is empty string",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"namespace": "",
				}},
			},
			expectedProblems: []string{
				"apps.0.namespace: String length must be greater than or equal to 1",
			},
		},
		{
			name: "app sources is empty array",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"sources": []promotion.Config{},
				}},
			},
			expectedProblems: []string{
				"apps.0.sources: Array must have at least 1 items",
			},
		},
		{
			name: "source missing repoURL",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "my-app",
					"sources": []promotion.Config{{
						"desiredRevision": "abc123",
					}},
				}},
			},
			expectedProblems: []string{
				"apps.0.sources.0: repoURL is required",
			},
		},
		{
			name: "source missing desiredRevision",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "my-app",
					"sources": []promotion.Config{{
						"repoURL": "https://github.com/example/repo",
					}},
				}},
			},
			expectedProblems: []string{
				"apps.0.sources.0: desiredRevision is required",
			},
		},
		{
			name: "source repoURL is empty string",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "my-app",
					"sources": []promotion.Config{{
						"repoURL":         "",
						"desiredRevision": "abc123",
					}},
				}},
			},
			expectedProblems: []string{
				"apps.0.sources.0.repoURL: String length must be greater than or equal to 1",
			},
		},
		{
			name: "source desiredRevision is empty string",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "my-app",
					"sources": []promotion.Config{{
						"repoURL":         "https://github.com/example/repo",
						"desiredRevision": "",
					}},
				}},
			},
			expectedProblems: []string{
				"apps.0.sources.0.desiredRevision: String length must be greater than or equal to 1",
			},
		},
		{
			name: "valid config with name",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "my-app",
					"sources": []promotion.Config{{
						"repoURL":         "https://github.com/example/repo",
						"desiredRevision": "abc123",
					}},
				}},
			},
		},
		{
			name: "valid config with selector",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"selector": promotion.Config{
						"matchLabels": promotion.Config{
							"env": "prod",
						},
					},
					"sources": []promotion.Config{{
						"repoURL":         "https://github.com/example/repo",
						"desiredRevision": "abc123",
					}},
				}},
			},
		},
		{
			name: "valid config with chart",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "my-app",
					"sources": []promotion.Config{{
						"repoURL":         "https://charts.example.com",
						"chart":           "my-chart",
						"desiredRevision": "1.2.3",
					}},
				}},
			},
		},
		{
			name: "additional properties not allowed on source",
			config: promotion.Config{
				"apps": []promotion.Config{{
					"name": "my-app",
					"sources": []promotion.Config{{
						"repoURL":              "https://github.com/example/repo",
						"desiredRevision":      "abc123",
						"updateTargetRevision": true,
					}},
				}},
			},
			expectedProblems: []string{
				"Additional property updateTargetRevision is not allowed",
			},
		},
	}

	runner := newArgocdAwaiter(promotion.StepRunnerCapabilities{
		ArgoCDClient: fake.NewFakeClient(),
	})
	awaiter, ok := runner.(*argocdAwaiter)
	require.True(t, ok)
	runValidationTests(t, awaiter.convert, tests)
}

func Test_argocdAwaiter_run(t *testing.T) {
	testCases := []struct {
		name    string
		runner  *argocdAwaiter
		stepCfg builtin.ArgoCDAwaitConfig
		assert  func(*testing.T, promotion.StepResult, error)
	}{
		{
			name:    "argo cd integration disabled",
			runner:  &argocdAwaiter{},
			stepCfg: builtin.ArgoCDAwaitConfig{},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusErrored,
					res.Status,
				)
				require.ErrorContains(
					t, err,
					"Argo CD integration is disabled",
				)
			},
		},
		{
			name: "error retrieving authorized applications",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						context.Context,
						*promotion.StepContext,
						builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						return nil, errors.New("something went wrong")
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{{}},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusErrored,
					res.Status,
				)
				require.ErrorContains(
					t, err, "something went wrong",
				)
			},
		},
		{
			name: "all revisions match single app",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						context.Context,
						*promotion.StepContext,
						builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						return []*argocd.Application{{
							Spec: argocd.ApplicationSpec{
								Source: &argocd.ApplicationSource{
									RepoURL: "https://github.com/example/repo",
								},
							},
							Status: argocd.ApplicationStatus{
								Sync: argocd.SyncStatus{
									Status:   argocd.SyncStatusCodeSynced,
									Revision: "abc123",
								},
							},
						}}, nil
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{{
					Name: "my-app",
					Sources: []builtin.ArgoCDAwaitSource{{
						RepoURL:         "https://github.com/example/repo",
						DesiredRevision: "abc123",
					}},
				}},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				require.NoError(t, err)
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusSucceeded,
					res.Status,
				)
				assert.Nil(t, res.RetryAfter)
			},
		},
		{
			name: "revision matches but app is OutOfSync",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						context.Context,
						*promotion.StepContext,
						builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						return []*argocd.Application{{
							Spec: argocd.ApplicationSpec{
								Source: &argocd.ApplicationSource{
									RepoURL: "https://github.com/example/repo",
								},
							},
							Status: argocd.ApplicationStatus{
								Sync: argocd.SyncStatus{
									Status:   argocd.SyncStatusCodeOutOfSync,
									Revision: "abc123",
								},
							},
						}}, nil
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{{
					Name: "my-app",
					Sources: []builtin.ArgoCDAwaitSource{{
						RepoURL:         "https://github.com/example/repo",
						DesiredRevision: "abc123",
					}},
				}},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				require.NoError(t, err)
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusRunning,
					res.Status,
				)
				assert.NotNil(t, res.RetryAfter)
			},
		},
		{
			name: "app has active sync operation",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						context.Context,
						*promotion.StepContext,
						builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						return []*argocd.Application{{
							Spec: argocd.ApplicationSpec{
								Source: &argocd.ApplicationSource{
									RepoURL: "https://github.com/example/repo",
								},
							},
							Status: argocd.ApplicationStatus{
								OperationState: &argocd.OperationState{
									Phase: argocd.OperationRunning,
								},
								Sync: argocd.SyncStatus{
									Status:   argocd.SyncStatusCodeSynced,
									Revision: "abc123",
								},
							},
						}}, nil
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{{
					Name: "my-app",
					Sources: []builtin.ArgoCDAwaitSource{{
						RepoURL:         "https://github.com/example/repo",
						DesiredRevision: "abc123",
					}},
				}},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				require.NoError(t, err)
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusRunning,
					res.Status,
				)
				assert.NotNil(t, res.RetryAfter)
			},
		},
		{
			name: "revision does not match",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						context.Context,
						*promotion.StepContext,
						builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						return []*argocd.Application{{
							Spec: argocd.ApplicationSpec{
								Source: &argocd.ApplicationSource{
									RepoURL: "https://github.com/example/repo",
								},
							},
							Status: argocd.ApplicationStatus{
								Sync: argocd.SyncStatus{
									Status:   argocd.SyncStatusCodeSynced,
									Revision: "old-revision",
								},
							},
						}}, nil
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{{
					Name: "my-app",
					Sources: []builtin.ArgoCDAwaitSource{{
						RepoURL:         "https://github.com/example/repo",
						DesiredRevision: "abc123",
					}},
				}},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				require.NoError(t, err)
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusRunning,
					res.Status,
				)
				assert.NotNil(t, res.RetryAfter)
			},
		},
		{
			name: "multi-source app all revisions match",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						context.Context,
						*promotion.StepContext,
						builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						return []*argocd.Application{{
							Spec: argocd.ApplicationSpec{
								Sources: argocd.ApplicationSources{
									{RepoURL: "https://github.com/example/repo1"},
									{RepoURL: "https://github.com/example/repo2"},
								},
							},
							Status: argocd.ApplicationStatus{
								Sync: argocd.SyncStatus{
									Status:    argocd.SyncStatusCodeSynced,
									Revisions: []string{"rev1", "rev2"},
								},
							},
						}}, nil
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{{
					Name: "my-app",
					Sources: []builtin.ArgoCDAwaitSource{
						{
							RepoURL:         "https://github.com/example/repo1",
							DesiredRevision: "rev1",
						},
						{
							RepoURL:         "https://github.com/example/repo2",
							DesiredRevision: "rev2",
						},
					},
				}},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				require.NoError(t, err)
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusSucceeded,
					res.Status,
				)
			},
		},
		{
			name: "multi-source app partial match",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						context.Context,
						*promotion.StepContext,
						builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						return []*argocd.Application{{
							Spec: argocd.ApplicationSpec{
								Sources: argocd.ApplicationSources{
									{RepoURL: "https://github.com/example/repo1"},
									{RepoURL: "https://github.com/example/repo2"},
								},
							},
							Status: argocd.ApplicationStatus{
								Sync: argocd.SyncStatus{
									Status:    argocd.SyncStatusCodeSynced,
									Revisions: []string{"rev1", "old-rev"},
								},
							},
						}}, nil
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{{
					Name: "my-app",
					Sources: []builtin.ArgoCDAwaitSource{
						{
							RepoURL:         "https://github.com/example/repo1",
							DesiredRevision: "rev1",
						},
						{
							RepoURL:         "https://github.com/example/repo2",
							DesiredRevision: "rev2",
						},
					},
				}},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				require.NoError(t, err)
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusRunning,
					res.Status,
				)
				assert.NotNil(t, res.RetryAfter)
			},
		},
		{
			name: "multiple apps mixed results",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						_ context.Context,
						_ *promotion.StepContext,
						appCfg builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						if appCfg.GetName() == "app1" {
							return []*argocd.Application{{
								Spec: argocd.ApplicationSpec{
									Source: &argocd.ApplicationSource{
										RepoURL: "https://github.com/example/repo",
									},
								},
								Status: argocd.ApplicationStatus{
									Sync: argocd.SyncStatus{
										Status:   argocd.SyncStatusCodeSynced,
										Revision: "abc123",
									},
								},
							}}, nil
						}
						return []*argocd.Application{{
							Spec: argocd.ApplicationSpec{
								Source: &argocd.ApplicationSource{
									RepoURL: "https://github.com/example/repo2",
								},
							},
							Status: argocd.ApplicationStatus{
								Sync: argocd.SyncStatus{
									Status:   argocd.SyncStatusCodeSynced,
									Revision: "old-rev",
								},
							},
						}}, nil
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{
					{
						Name: "app1",
						Sources: []builtin.ArgoCDAwaitSource{{
							RepoURL:         "https://github.com/example/repo",
							DesiredRevision: "abc123",
						}},
					},
					{
						Name: "app2",
						Sources: []builtin.ArgoCDAwaitSource{{
							RepoURL:         "https://github.com/example/repo2",
							DesiredRevision: "def456",
						}},
					},
				},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				require.NoError(t, err)
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusRunning,
					res.Status,
				)
				assert.NotNil(t, res.RetryAfter)
			},
		},
		{
			name: "no sources configured uses empty desired revisions",
			runner: &argocdAwaiter{
				argocdBase: argocdBase{
					argocdClient: fake.NewFakeClient(),
					getAuthorizedApplicationsFn: func(
						context.Context,
						*promotion.StepContext,
						builtin.ArgoCDAppReference,
					) ([]*argocd.Application, error) {
						return []*argocd.Application{{
							Spec: argocd.ApplicationSpec{
								Source: &argocd.ApplicationSource{
									RepoURL: "https://github.com/example/repo",
								},
							},
							Status: argocd.ApplicationStatus{
								Sync: argocd.SyncStatus{
									Status:   argocd.SyncStatusCodeSynced,
									Revision: "anything",
								},
							},
						}}, nil
					},
				},
			},
			stepCfg: builtin.ArgoCDAwaitConfig{
				Apps: []builtin.ArgoCDAwaitApp{{
					Name: "my-app",
				}},
			},
			assert: func(t *testing.T, res promotion.StepResult, err error) {
				require.NoError(t, err)
				// With no sources configured, all desired revisions are
				// empty strings, which are skipped -- so the check passes.
				assert.Equal(
					t,
					kargoapi.PromotionStepStatusSucceeded,
					res.Status,
				)
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.runner.run(
				context.Background(),
				&promotion.StepContext{},
				tc.stepCfg,
			)
			tc.assert(t, res, err)
		})
	}
}

func Test_argocdAwaiter_checkRevisions(t *testing.T) {
	a := &argocdAwaiter{}
	testCases := []struct {
		name             string
		app              *argocd.Application
		desiredRevisions []string
		expected         bool
	}{
		{
			name: "single source matches",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         true,
		},
		{
			name: "single source does not match",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "old-rev",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         false,
		},
		{
			name: "revision matches but app is OutOfSync",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeOutOfSync,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         false,
		},
		{
			name: "revision matches but sync status is Unknown",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeUnknown,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         false,
		},
		{
			name: "pending operation",
			app: &argocd.Application{
				Operation: &argocd.Operation{},
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         false,
		},
		{
			name: "operation in progress",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					OperationState: &argocd.OperationState{
						Phase: argocd.OperationRunning,
					},
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         false,
		},
		{
			name: "operation terminating",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					OperationState: &argocd.OperationState{
						Phase: argocd.OperationTerminating,
					},
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         false,
		},
		{
			name: "operation completed successfully without sync result",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					OperationState: &argocd.OperationState{
						Phase: argocd.OperationSucceeded,
					},
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         true,
		},
		{
			name: "last sync result revision matches",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					OperationState: &argocd.OperationState{
						Phase: argocd.OperationSucceeded,
						SyncResult: &argocd.SyncOperationResult{
							Revision: "abc123",
						},
					},
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeOutOfSync,
						Revision: "old-rev",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         true,
		},
		{
			name: "last sync result does not match but sync status does",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					OperationState: &argocd.OperationState{
						Phase: argocd.OperationSucceeded,
						SyncResult: &argocd.SyncOperationResult{
							Revision: "old-rev",
						},
					},
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         true,
		},
		{
			name: "neither last sync result nor sync status match",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					OperationState: &argocd.OperationState{
						Phase: argocd.OperationSucceeded,
						SyncResult: &argocd.SyncOperationResult{
							Revision: "old-rev",
						},
					},
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "wrong-rev",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         false,
		},
		{
			name: "last sync result does not match and sync status is OutOfSync",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					OperationState: &argocd.OperationState{
						Phase: argocd.OperationSucceeded,
						SyncResult: &argocd.SyncOperationResult{
							Revision: "old-rev",
						},
					},
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeOutOfSync,
						Revision: "abc123",
					},
				},
			},
			desiredRevisions: []string{"abc123"},
			expected:         false,
		},
		{
			name: "last sync result multi-source revisions match",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					OperationState: &argocd.OperationState{
						Phase: argocd.OperationSucceeded,
						SyncResult: &argocd.SyncOperationResult{
							Revisions: []string{"rev1", "rev2"},
						},
					},
					Sync: argocd.SyncStatus{
						Status:    argocd.SyncStatusCodeOutOfSync,
						Revisions: []string{"old1", "old2"},
					},
				},
			},
			desiredRevisions: []string{"rev1", "rev2"},
			expected:         true,
		},
		{
			name: "multi-source all match",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:    argocd.SyncStatusCodeSynced,
						Revisions: []string{"rev1", "rev2"},
					},
				},
			},
			desiredRevisions: []string{"rev1", "rev2"},
			expected:         true,
		},
		{
			name: "multi-source partial match",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:    argocd.SyncStatusCodeSynced,
						Revisions: []string{"rev1", "old"},
					},
				},
			},
			desiredRevisions: []string{"rev1", "rev2"},
			expected:         false,
		},
		{
			name: "empty desired revision is skipped",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:    argocd.SyncStatusCodeSynced,
						Revisions: []string{"rev1", "anything"},
					},
				},
			},
			desiredRevisions: []string{"rev1", ""},
			expected:         true,
		},
		{
			name: "desired index out of range",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "rev1",
					},
				},
			},
			desiredRevisions: []string{"rev1", "rev2"},
			expected:         false,
		},
		{
			name: "no desired revisions and synced",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeSynced,
						Revision: "anything",
					},
				},
			},
			desiredRevisions: nil,
			expected:         true,
		},
		{
			name: "no desired revisions but not synced",
			app: &argocd.Application{
				Status: argocd.ApplicationStatus{
					Sync: argocd.SyncStatus{
						Status:   argocd.SyncStatusCodeOutOfSync,
						Revision: "anything",
					},
				},
			},
			desiredRevisions: nil,
			expected:         false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := a.checkRevisions(tc.app, tc.desiredRevisions)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func Test_argocdAwaiter_getDesiredRevisions(t *testing.T) {
	a := &argocdAwaiter{}
	testCases := []struct {
		name     string
		appCfg   *builtin.ArgoCDAwaitApp
		app      *argocd.Application
		expected []string
	}{
		{
			name:     "nil app",
			appCfg:   &builtin.ArgoCDAwaitApp{},
			app:      nil,
			expected: nil,
		},
		{
			name:   "no sources in app",
			appCfg: &builtin.ArgoCDAwaitApp{},
			app: &argocd.Application{
				Spec: argocd.ApplicationSpec{},
			},
			expected: nil,
		},
		{
			name: "single source matched",
			appCfg: &builtin.ArgoCDAwaitApp{
				Sources: []builtin.ArgoCDAwaitSource{{
					RepoURL:         "https://github.com/example/repo",
					DesiredRevision: "abc123",
				}},
			},
			app: &argocd.Application{
				Spec: argocd.ApplicationSpec{
					Source: &argocd.ApplicationSource{
						RepoURL: "https://github.com/example/repo",
					},
				},
			},
			expected: []string{"abc123"},
		},
		{
			name: "single source not matched",
			appCfg: &builtin.ArgoCDAwaitApp{
				Sources: []builtin.ArgoCDAwaitSource{{
					RepoURL:         "https://github.com/other/repo",
					DesiredRevision: "abc123",
				}},
			},
			app: &argocd.Application{
				Spec: argocd.ApplicationSpec{
					Source: &argocd.ApplicationSource{
						RepoURL: "https://github.com/example/repo",
					},
				},
			},
			expected: []string{""},
		},
		{
			name: "multi-source partial match",
			appCfg: &builtin.ArgoCDAwaitApp{
				Sources: []builtin.ArgoCDAwaitSource{{
					RepoURL:         "https://github.com/example/repo2",
					DesiredRevision: "rev2",
				}},
			},
			app: &argocd.Application{
				Spec: argocd.ApplicationSpec{
					Sources: argocd.ApplicationSources{
						{RepoURL: "https://github.com/example/repo1"},
						{RepoURL: "https://github.com/example/repo2"},
					},
				},
			},
			expected: []string{"", "rev2"},
		},
		{
			name: "chart source matched",
			appCfg: &builtin.ArgoCDAwaitApp{
				Sources: []builtin.ArgoCDAwaitSource{{
					RepoURL:         "https://charts.example.com",
					Chart:           "my-chart",
					DesiredRevision: "1.2.3",
				}},
			},
			app: &argocd.Application{
				Spec: argocd.ApplicationSpec{
					Source: &argocd.ApplicationSource{
						RepoURL: "https://charts.example.com",
						Chart:   "my-chart",
					},
				},
			},
			expected: []string{"1.2.3"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := a.getDesiredRevisions(tc.appCfg, tc.app)
			assert.Equal(t, tc.expected, result)
		})
	}
}
