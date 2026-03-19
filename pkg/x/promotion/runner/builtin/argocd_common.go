package builtin

// ArgoCDAppReference is implemented by any config type that identifies
// Argo CD Applications by name or label selector.
type ArgoCDAppReference interface {
	GetName() string
	GetNamespace() string
	GetSelector() *ArgoCDAppSelector
}
