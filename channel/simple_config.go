package channel

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	configRoot    = "cluster"
	poolConfigDir = "node-pools"
	etcdConfigDir = "etcd"
	defaultsFile  = "config-defaults.yaml"
	manifestsDir  = "manifests"
	deletionsFile = "deletions.yaml"
)

type SimpleConfig struct {
	pathPrefix  string
	baseDir     string
	allowDelete bool
}

func NewSimpleConfig(sourceName string, baseDir string, allowDelete bool) (*SimpleConfig, error) {
	abspath, err := filepath.Abs(path.Clean(baseDir))
	if err != nil {
		return nil, err
	}
	return &SimpleConfig{
		pathPrefix:  sourceName + "/",
		baseDir:     abspath,
		allowDelete: allowDelete,
	}, nil
}

func (c *SimpleConfig) readManifest(manifestDirectory string, name string) (Manifest, error) {
	filePath := path.Join(c.baseDir, manifestDirectory, name)
	res, err := ioutil.ReadFile(filePath)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Path:     path.Join(c.pathPrefix, manifestDirectory, name),
		Contents: res,
	}, nil
}

func (c *SimpleConfig) StackManifest(manifestName string) (Manifest, error) {
	return c.readManifest(configRoot, manifestName)
}

func (c *SimpleConfig) EtcdManifest(manifestName string) (Manifest, error) {
	return c.readManifest(path.Join(configRoot, etcdConfigDir), manifestName)
}

func (c *SimpleConfig) NodePoolManifest(profileName string, manifestName string) (Manifest, error) {
	return c.readManifest(path.Join(configRoot, poolConfigDir, profileName), manifestName)
}

func (c *SimpleConfig) DefaultsManifests() ([]Manifest, error) {
	res, err := c.readManifest(configRoot, defaultsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return []Manifest{res}, nil
}

func (c *SimpleConfig) DeletionsManifests() ([]Manifest, error) {
	res, err := c.readManifest(path.Join(configRoot, manifestsDir), deletionsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return []Manifest{res}, nil
}

func (c *SimpleConfig) Components() ([]Component, error) {
	var result []Component

	componentsDir := path.Join(c.baseDir, configRoot, manifestsDir)
	components, err := ioutil.ReadDir(componentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, component := range components {
		if !component.IsDir() {
			continue
		}

		var manifests []Manifest

		componentDir := path.Join(componentsDir, component.Name())
		files, err := ioutil.ReadDir(componentDir)
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".yaml") && !strings.HasSuffix(file.Name(), ".yml") {
				continue
			}
			manifest, err := c.readManifest(path.Join(configRoot, manifestsDir, component.Name()), file.Name())
			if err != nil {
				return nil, err
			}

			manifests = append(manifests, manifest)
		}
		result = append(result, Component{
			Name:      component.Name(),
			Manifests: manifests,
		})
	}
	return result, nil
}

func (c *SimpleConfig) Delete() error {
	if !c.allowDelete {
		return nil
	}
	return os.RemoveAll(c.baseDir)
}
