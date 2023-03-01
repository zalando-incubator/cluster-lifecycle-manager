package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/cenkalti/backoff"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	yaml "gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

func (p *clusterpyProvisioner) applyKubectl(ctx context.Context, logger *log.Entry, cluster *api.Cluster, renderedManifests []manifestPackage) error {
	token, err := p.tokenSource.Token()
	if err != nil {
		return errors.Wrapf(err, "no valid token")
	}

	for _, m := range renderedManifests {
		logger := logger.WithField("module", m.name)

		args := []string{
			"kubectl",
			"apply",
			fmt.Sprintf("--server=%s", cluster.APIServerURL),
			fmt.Sprintf("--token=%s", token.AccessToken),
			"-f",
			"-",
		}

		newApplyCommand := func() *exec.Cmd {
			cmd := exec.Command(args[0], args[1:]...)
			// prevent kubectl to find the in-cluster config
			cmd.Env = []string{}
			return cmd
		}

		if p.dryRun {
			logger.Debug(newApplyCommand())
		} else {
			applyManifest := func() error {
				cmd := newApplyCommand()
				cmd.Stdin = strings.NewReader(strings.Join(m.manifests, "---\n"))
				_, err := p.execManager.Run(ctx, logger, cmd)
				return err
			}
			err = backoff.Retry(applyManifest, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), maxApplyRetries))
			if err != nil {
				return errors.Wrapf(err, "kubectl apply failed for %s", m.name)
			}
		}
	}

	return nil
}

func (p *clusterpyProvisioner) applyServerSide(ctx context.Context, logger *log.Entry, cluster *api.Cluster, renderedManifests []manifestPackage) error {
	typedClient, err := kubernetes.NewClient(cluster.APIServerURL, p.tokenSource)
	if err != nil {
		return err
	}

	dynamicClient, err := kubernetes.NewDynamicClient(cluster.APIServerURL, p.tokenSource)
	if err != nil {
		return err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(typedClient.Discovery()))

	for _, pkg := range renderedManifests {
		logger := logger.WithField("module", pkg.name)
		for _, manifest := range pkg.manifests {
			decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 1000)
			for {
				var obj unstructured.Unstructured
				err := decoder.Decode(&obj)
				if err != nil {
					if err == io.EOF {
						break
					}
					logger.Errorf("Unable to parse a resource: %v", err)
					return err
				}

				gvk := obj.GroupVersionKind()

				logger = logger.WithFields(log.Fields{
					"group": gvk.GroupVersion().String(),
					"kind":  gvk.Kind,
				})
				if obj.GetNamespace() != "" {
					logger = logger.WithField("namespace", obj.GetNamespace())
				}

				// HACK: Disable SSA for APIService resources due to a bug in Kubernetes (https://github.com/kubernetes/kubernetes/issues/89264)
				// TODO drop once we update to 1.21
				if gvk.Group == "apiregistration.k8s.io" && gvk.Kind == "APIService" {
					marshalled, remarshalErr := yaml.Marshal(obj.Object)
					if remarshalErr != nil {
						logger.Errorf("Failed to remarshal %s %s: %v", gvk.Kind, obj.GetName(), remarshalErr)
						return remarshalErr
					}
					err = p.applyKubectl(ctx, logger, cluster, []manifestPackage{
						{
							name:      pkg.name,
							manifests: []string{string(marshalled)},
						},
					})
				} else {
					err = applyServerSideSingle(ctx, dynamicClient, mapper, obj)
				}

				if err != nil {
					logger.Errorf("Failed to apply the manifest for %s %s: %v", gvk.Kind, obj.GetName(), err)
					return err
				}
				logger.Infof("Applied the manifest for %s %s", gvk.Kind, obj.GetName())
			}
		}
	}

	return nil
}

func injectLastApplied(object unstructured.Unstructured) error {
	// kubectl always sets metadata.annotations in last-applied-configuration to an empty object, let's try to do the same
	err := unstructured.SetNestedField(object.Object, "", "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
	if err != nil {
		return err
	}

	// Remove the old value of last-applied-configuration first
	unstructured.RemoveNestedField(object.Object, "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")

	// NewEncoder().Encode instead of Marshal to keep the newline at the end
	buf := bytes.Buffer{}
	err = json.NewEncoder(&buf).Encode(object.Object)
	if err != nil {
		return err
	}
	return unstructured.SetNestedField(object.Object, buf.String(), "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
}

func applyServerSideSingle(ctx context.Context, dynamicClient dynamic.Interface, mapper *restmapper.DeferredDiscoveryRESTMapper, object unstructured.Unstructured) error {
	gvk := object.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	var objIface dynamic.ResourceInterface

	resourceIface := dynamicClient.Resource(mapping.Resource)
	if object.GetNamespace() == "" {
		objIface = resourceIface
	} else {
		objIface = resourceIface.Namespace(object.GetNamespace())
	}

	err = injectLastApplied(object)
	if err != nil {
		return err
	}

	// Inject the 'last-applied-configuration' annotation so we could switch back

	marshalled, err := yaml.Marshal(object.Object)
	if err != nil {
		return err
	}
	log.Infof("Final result: \n%s", string(marshalled))

	force := true
	_, err = objIface.Patch(ctx, object.GetName(), types.ApplyPatchType, marshalled, metav1.PatchOptions{
		Force:        &force,
		FieldManager: "cluster-lifecycle-manager",
	})
	return err
}
