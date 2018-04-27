package provisioner

// Applier defines an interface which given a path can apply manifests to a
// kubernetes cluster.
type Applier interface {
	Apply(path string) error
}
