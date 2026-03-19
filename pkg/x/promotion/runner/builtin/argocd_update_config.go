package builtin

// GetName returns the name of the Argo CD Application to update.
func (a *ArgoCDAppUpdate) GetName() string { return a.Name }

// GetNamespace returns the namespace of the Argo CD Application to update.
func (a *ArgoCDAppUpdate) GetNamespace() string { return a.Namespace }

// GetSelector returns the label selector for matching Argo CD Applications
// to update.
func (a *ArgoCDAppUpdate) GetSelector() *ArgoCDAppSelector {
	return a.Selector
}
