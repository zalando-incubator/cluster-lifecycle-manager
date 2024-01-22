package provisioner

import (
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

func (d *templateData) deprecatedKey(key string) {
	log.WithFields(log.Fields{
		"template": d.file,
		"key":      key,
	}).Warnf("Use of deprecated key")
}

func (d *templateData) Alias() string {
	d.deprecatedKey("Alias")
	return d.Cluster.Alias
}

func (d *templateData) APIServerURL() string {
	d.deprecatedKey("APIServerURL")
	return d.Cluster.APIServerURL
}

func (d *templateData) Channel() string {
	d.deprecatedKey("Channel")
	return d.Cluster.Channel
}

func (d *templateData) ConfigItems() map[string]string {
	d.deprecatedKey("ConfigItems")
	return d.Cluster.ConfigItems
}

func (d *templateData) CriticalityLevel() int32 {
	d.deprecatedKey("CriticalityLevel")
	return d.Cluster.CriticalityLevel
}

func (d *templateData) Environment() string {
	d.deprecatedKey("Environment")
	return d.Cluster.Environment
}

func (d *templateData) ID() string {
	d.deprecatedKey("ID")
	return d.Cluster.ID
}

func (d *templateData) InfrastructureAccount() string {
	d.deprecatedKey("InfrastructureAccount")
	return d.Cluster.InfrastructureAccount
}

func (d *templateData) LifecycleStatus() string {
	d.deprecatedKey("LifecycleStatus")
	return d.Cluster.LifecycleStatus
}

func (d *templateData) LocalID() string {
	d.deprecatedKey("LocalID")
	return d.Cluster.LocalID
}

func (d *templateData) NodePools() []*api.NodePool {
	d.deprecatedKey("NodePools")
	return d.Cluster.NodePools
}

func (d *templateData) Region() string {
	d.deprecatedKey("Region")
	return d.Cluster.Region
}

func (d *templateData) Owner() string {
	d.deprecatedKey("Owner")
	return d.Cluster.Owner
}

func (d *templateData) UserData() string {
	d.deprecatedKey("UserData")
	v, _ := d.Values["UserData"].(string)
	return v
}

func (d *templateData) S3GeneratedFilesPath() string {
	d.deprecatedKey("S3GeneratedFilesPath")
	v, _ := d.Values["S3GeneratedFilesPath"].(string)
	return v
}
