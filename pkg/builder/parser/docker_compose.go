// RAINBOND, Application Management Platform
// Copyright (C) 2014-2017 Goodrain Co., Ltd.

// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version. For any non-GPL usage of Rainbond,
// one or multiple Commercial Licenses authorized by Goodrain Co., Ltd.
// must be obtained first.

// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package parser

import (
	"strings"

	"github.com/goodrain/rainbond/pkg/builder/parser/compose"
	"github.com/goodrain/rainbond/pkg/builder/sources"
	"github.com/goodrain/rainbond/pkg/db/model"
	"github.com/goodrain/rainbond/pkg/event"

	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/client"
)

//DockerComposeParse docker compose 文件解析
type DockerComposeParse struct {
	services     map[string]*serviceInfoFromDC
	errors       []ParseError
	dockerclient *client.Client
	logger       event.Logger
	source       string
}
type serviceInfoFromDC struct {
	ports   map[int]*Port
	volumes map[string]*Volume
	envs    map[string]*Env
	source  string
	memory  int
	image   Image
	args    []string
	depends []string
}

//GetPorts 获取端口列表
func (d *serviceInfoFromDC) GetPorts() (ports []Port) {
	for _, cv := range d.ports {
		ports = append(ports, *cv)
	}
	return ports
}

//GetVolumes 获取存储列表
func (d *serviceInfoFromDC) GetVolumes() (volumes []Volume) {
	for _, cv := range d.volumes {
		volumes = append(volumes, *cv)
	}
	return
}

//GetEnvs 环境变量
func (d *serviceInfoFromDC) GetEnvs() (envs []Env) {
	for _, cv := range d.envs {
		envs = append(envs, *cv)
	}
	return
}

//CreateDockerComposeParse create parser
func CreateDockerComposeParse(source string, dockerclient *client.Client, logger event.Logger) Parser {
	return &DockerComposeParse{
		source:       source,
		dockerclient: dockerclient,
		logger:       logger,
		services:     make(map[string]*serviceInfoFromDC),
	}
}

//Parse 解码
func (d *DockerComposeParse) Parse() ParseErrorList {
	if d.source == "" {
		d.errappend(Errorf(FatalError, "source can not be empty"))
		return d.errors
	}
	comp := compose.Compose{}
	co, err := comp.LoadBytes([][]byte{[]byte(d.source)})
	if err != nil {
		d.errappend(Errorf(FatalError, err.Error()))
		return d.errors
	}
	for kev, sc := range co.ServiceConfigs {
		ports := make(map[int]*Port)
		for _, p := range sc.Port {
			pro := string(p.Protocol)
			if pro != "udp" {
				pro = GetPortProtocol(int(p.ContainerPort))
			}
			ports[int(p.ContainerPort)] = &Port{
				ContainerPort: int(p.ContainerPort),
				Protocol:      pro,
			}
		}
		volumes := make(map[string]*Volume)
		for _, v := range sc.Volumes {
			volumes[v.MountPath] = &Volume{
				VolumePath: v.MountPath,
				VolumeType: model.ShareFileVolumeType.String(),
			}
		}
		envs := make(map[string]*Env)
		for _, e := range sc.Environment {
			envs[e.Name] = &Env{
				Name:  e.Name,
				Value: e.Value,
			}
		}
		service := serviceInfoFromDC{
			ports:   ports,
			volumes: volumes,
			envs:    envs,
			memory:  int(sc.MemLimit),
			image:   parseImageName(sc.Image),
			args:    sc.Args,
			depends: sc.Links,
		}
		if sc.DependsON != nil {
			service.depends = sc.DependsON
		}
		d.services[kev] = &service
	}
	for _, service := range d.services {
		//获取镜像，验证是否存在
		imageInspect, err := sources.ImagePull(d.dockerclient, service.image.String(), types.ImagePullOptions{}, d.logger, 5)
		if err != nil {
			d.errappend(Errorf(FatalError, err.Error()))
			return d.errors
		}
		if imageInspect != nil && imageInspect.ContainerConfig != nil {
			for _, env := range imageInspect.ContainerConfig.Env {
				envinfo := strings.Split(env, "=")
				if len(envinfo) == 2 {
					if _, ok := service.envs[envinfo[0]]; !ok {
						service.envs[envinfo[0]] = &Env{Name: envinfo[0], Value: envinfo[1]}
					}
				}
			}
			for k := range imageInspect.ContainerConfig.Volumes {
				if _, ok := service.volumes[k]; !ok {
					service.volumes[k] = &Volume{VolumePath: k, VolumeType: model.ShareFileVolumeType.String()}
				}
			}
			for k := range imageInspect.ContainerConfig.ExposedPorts {
				proto := k.Proto()
				port := k.Int()
				if proto != "udp" {
					proto = GetPortProtocol(port)
				}
				if _, ok := service.ports[port]; ok {
					service.ports[port].Protocol = proto
				} else {
					service.ports[port] = &Port{Protocol: proto, ContainerPort: port}
				}
			}
		}
	}
	return d.errors
}

func (d *DockerComposeParse) errappend(pe ParseError) {
	d.errors = append(d.errors, pe)
}

//GetServiceInfo 获取service info
func (d *DockerComposeParse) GetServiceInfo() []ServiceInfo {
	var sis []ServiceInfo
	for _, service := range d.services {
		si := ServiceInfo{
			Ports:          service.GetPorts(),
			Envs:           service.GetEnvs(),
			Volumes:        service.GetVolumes(),
			Image:          service.image,
			Args:           service.args,
			DependServices: service.depends,
			Memory:         service.memory,
		}
		sis = append(sis, si)
	}
	return sis
}

//GetImage 获取镜像名
func (d *DockerComposeParse) GetImage() Image {
	return Image{}
}
