/*
Copyright 2022 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mpi

import (
	"flag"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	MpiPluginName = "mpi"
	DefaultPort   = 22
	DefaultMaster = "master"
	DefaultWorker = "worker"
	MpiHost       = "MPI_HOST"
)

type mpiPlugin struct {
	mpiArguments []string
	clientset    pluginsinterface.PluginClientset
	masterName   string
	workerName   string
	port         int
}

// New creates mpi plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	mp := mpiPlugin{mpiArguments: arguments, clientset: client}
	mp.addFlags()
	return &mp
}

func NewInstance(arguments []string) mpiPlugin {
	mp := mpiPlugin{mpiArguments: arguments}
	mp.addFlags()
	return mp
}

func (mp *mpiPlugin) addFlags() {
	flagSet := flag.NewFlagSet(mp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&mp.masterName, "master", DefaultMaster, "name of master role task")
	flagSet.StringVar(&mp.workerName, "worker", DefaultWorker, "name of worker role task")
	flagSet.IntVar(&mp.port, "port", DefaultPort, "open port for containers")
	if err := flagSet.Parse(mp.mpiArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", mp.Name(), err)
	}
}

func (mp *mpiPlugin) Name() string {
	return MpiPluginName
}

func (mp *mpiPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	isMaster := false
	workerHosts := ""
	env := v1.EnvVar{}
	if helpers.GetTaskKey(pod) == mp.masterName {
		workerHosts = mp.generateTaskHosts(job.Spec.Tasks[helpers.GetTasklndexUnderJob(mp.workerName, job)], job.Name)
		env = v1.EnvVar{
			Name:  MpiHost,
			Value: workerHosts,
		}

		isMaster = true
	}

	// open port for ssh and add MPI_HOST env for master task
	for index, ic := range pod.Spec.InitContainers {
		mp.openContainerPort(&ic, index, pod, true)
		if isMaster {
			pod.Spec.InitContainers[index].Env = append(pod.Spec.InitContainers[index].Env, env)
		}
	}

	for index, c := range pod.Spec.Containers {
		mp.openContainerPort(&c, index, pod, false)
		if isMaster {
			pod.Spec.Containers[index].Env = append(pod.Spec.Containers[index].Env, env)
		}
	}

	return nil
}

func (mp *mpiPlugin) generateTaskHosts(task batch.TaskSpec, jobName string) string {
	hosts := ""
	for i := 0; i < int(task.Replicas); i++ {
		hostName := task.Template.Spec.Hostname
		subdomain := task.Template.Spec.Subdomain
		if len(hostName) == 0 {
			hostName = helpers.MakePodName(jobName, task.Name, i)
		}
		if len(subdomain) == 0 {
			subdomain = jobName
		}
		hosts = hosts + hostName + "." + subdomain + ","
		if len(task.Template.Spec.Hostname) != 0 {
			break
		}
	}
	return hosts[:len(hosts)-1]
}

func (mp *mpiPlugin) openContainerPort(c *v1.Container, index int, pod *v1.Pod, isInitContainer bool) {
	SSHPortRight := false
	for _, p := range c.Ports {
		if p.ContainerPort == int32(mp.port) {
			SSHPortRight = true
			break
		}
	}
	if !SSHPortRight {
		sshPort := v1.ContainerPort{
			Name:          "mpijob-port",
			ContainerPort: int32(mp.port),
		}
		if isInitContainer {
			pod.Spec.InitContainers[index].Ports = append(pod.Spec.InitContainers[index].Ports, sshPort)
		} else {
			pod.Spec.Containers[index].Ports = append(pod.Spec.Containers[index].Ports, sshPort)
		}
	}
}

func (mp *mpiPlugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+mp.Name()] == mp.Name() {
		return nil
	}
	job.Status.ControlledResources["plugin-"+mp.Name()] = mp.Name()
	return nil
}

func (mp *mpiPlugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+mp.Name()] != mp.Name() {
		return nil
	}
	delete(job.Status.ControlledResources, "plugin-"+mp.Name())
	return nil
}

func (mp *mpiPlugin) OnJobUpdate(job *batch.Job) error {
	return nil
}

func (mp *mpiPlugin) GetMasterName() string {
	return mp.masterName
}

func (mp *mpiPlugin) GetWorkerName() string {
	return mp.workerName
}

func (mp *mpiPlugin) GetMpiArguments() []string {
	return mp.mpiArguments
}
