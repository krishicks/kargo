package builtin

// ArgoCDAwaitConfig represents the configuration for the argocd-await
// promotion step.
type ArgoCDAwaitConfig struct {
	Apps []ArgoCDAwaitApp `json:"apps"`
}

// ArgoCDAwaitApp identifies an Argo CD Application to await by name or
// label selector, along with the sources whose revisions should be checked.
type ArgoCDAwaitApp struct {
	// Name specifies the exact name of an Argo CD Application resource to
	// await. Mutually exclusive with Selector.
	Name string `json:"name,omitempty"`
	// Namespace specifies the namespace of an Argo CD Application resource.
	// If left unspecified, the controller's configured default is used.
	Namespace string `json:"namespace,omitempty"`
	// Selector specifies a label selector to match Argo CD Application
	// resources. Mutually exclusive with Name.
	Selector *ArgoCDAppSelector `json:"selector,omitempty"`
	// Sources describes which Application sources to check for desired
	// revisions.
	Sources []ArgoCDAwaitSource `json:"sources,omitempty"`
}

// GetName returns the name of the Argo CD Application to await.
func (a *ArgoCDAwaitApp) GetName() string { return a.Name }

// GetNamespace returns the namespace of the Argo CD Application to await.
func (a *ArgoCDAwaitApp) GetNamespace() string { return a.Namespace }

// GetSelector returns the label selector for matching Argo CD Applications
// to await.
func (a *ArgoCDAwaitApp) GetSelector() *ArgoCDAppSelector {
	return a.Selector
}

// ArgoCDAwaitSource identifies a source within an Argo CD Application and
// the revision it should be synced to.
type ArgoCDAwaitSource struct {
	// RepoURL identifies which of an Argo CD Application's sources to check.
	RepoURL string `json:"repoURL"`
	// Chart, if applicable, identifies a specific chart within the Helm chart
	// repository specified by RepoURL.
	Chart string `json:"chart,omitempty"`
	// DesiredRevision is the revision the source should be observably synced
	// to.
	DesiredRevision string `json:"desiredRevision"`
}
