package exporters

import (
	"strconv"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/agents"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/networkipavailabilities"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/portsbinding"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/provider"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	"github.com/prometheus/client_golang/prometheus"
)

// NeutronExporter : extends BaseOpenStackExporter
type NeutronExporter struct {
	BaseOpenStackExporter
}

var defaultNeutronMetrics = []Metric{
	{Name: "floating_ips", Labels: []string{"region_name"}, Fn: ListFloatingIps},
	{Name: "floating_ips_associated_not_active", Labels: []string{"region_name"}},
	{Name: "floating_ip", Labels: []string{"id", "floating_network_id", "router_id", "status", "project_id", "floating_ip_address", "region_name"}},
	{Name: "network", Labels: []string{"id", "name", "admin_state_up", "status", "tenant_id", "project_id", "region_name", "type", "physical_network", "seg_id"}, Fn: ListNetworks},
	{Name: "networks", Labels: []string{"region_name"}},
	{Name: "security_groups", Labels: []string{"region_name"}, Fn: ListSecGroups},
	{Name: "subnets", Labels: []string{"region_name"}, Fn: ListSubnets},
	{Name: "port", Labels: []string{"uuid", "network_id", "mac_address", "device_owner", "status", "binding_vif_type", "admin_state_up", "device_id", "region_name"}, Fn: ListPorts},
	{Name: "ports", Labels: []string{"region_name"}},
	{Name: "ports_no_ips", Labels: []string{"region_name"}},
	{Name: "ports_lb_not_active", Labels: []string{"region_name"}},
	{Name: "router", Labels: []string{"id", "name", "project_id", "admin_state_up", "status", "external_network_id", "region_name"}},
	{Name: "routers", Labels: []string{"region_name"}, Fn: ListRouters},
	{Name: "routers_not_active", Labels: []string{"region_name"}},
	{Name: "l3_agent_of_router", Labels: []string{"router_id", "l3_agent_id", "ha_state", "agent_alive", "agent_admin_up", "agent_host", "region_name"}},
	{Name: "agent_state", Labels: []string{"id", "hostname", "service", "adminState", "region_name"}, Fn: ListAgentStates},
	{Name: "network_ip_availabilities_total", Labels: []string{"network_id", "network_name", "ip_version", "cidr", "subnet_name", "project_id", "region_name"}, Fn: ListNetworkIPAvailabilities},
	{Name: "network_ip_availabilities_used", Labels: []string{"network_id", "network_name", "ip_version", "cidr", "subnet_name", "project_id", "region_name"}},
}

// NewNeutronExporter : returns a pointer to NeutronExporter
func NewNeutronExporter(config *ExporterConfig) (*NeutronExporter, error) {
	exporter := NeutronExporter{
		BaseOpenStackExporter{
			Name:           "neutron",
			ExporterConfig: *config,
		},
	}

	for _, metric := range defaultNeutronMetrics {
		if exporter.isDeprecatedMetric(&metric) {
			continue
		}
		if !exporter.isSlowMetric(&metric) {
			exporter.AddMetric(metric.Name, metric.Fn, metric.Labels, metric.DeprecatedVersion, nil)
		}
	}

	return &exporter, nil
}

// ListFloatingIps : count total number of instantiated FloatingIPs and those that are associated to private IP but not in ACTIVE state
func ListFloatingIps(exporter *BaseOpenStackExporter, ch chan<- prometheus.Metric) error {
	var allFloatingIPs []floatingips.FloatingIP

	allPagesFloatingIPs, err := floatingips.List(exporter.Client, floatingips.ListOpts{}).AllPages()
	if err != nil {
		return err
	}

	allFloatingIPs, err = floatingips.ExtractFloatingIPs(allPagesFloatingIPs)
	if err != nil {
		return err
	}

	failedFIPs := 0
	for _, fip := range allFloatingIPs {
		ch <- prometheus.MustNewConstMetric(exporter.Metrics["floating_ip"].Metric,
			prometheus.GaugeValue, 1,
			fip.ID, fip.FloatingNetworkID, fip.RouterID, fip.Status, fip.ProjectID, fip.FloatingIP,
			endpointOpts["network"].Region)
		if fip.FixedIP != "" {
			if fip.Status != "ACTIVE" {
				failedFIPs = failedFIPs + 1
			}
		}
	}

	ch <- prometheus.MustNewConstMetric(exporter.Metrics["floating_ips"].Metric,
		prometheus.GaugeValue, float64(len(allFloatingIPs)),
		endpointOpts["network"].Region)
	ch <- prometheus.MustNewConstMetric(exporter.Metrics["floating_ips_associated_not_active"].Metric,
		prometheus.GaugeValue, float64(failedFIPs),
		endpointOpts["network"].Region)

	return nil
}

// ListAgentStates : list agent state per node
func ListAgentStates(exporter *BaseOpenStackExporter, ch chan<- prometheus.Metric) error {
	var allAgents []agents.Agent

	allPagesAgents, err := agents.List(exporter.Client, agents.ListOpts{}).AllPages()
	if err != nil {
		return err
	}

	allAgents, err = agents.ExtractAgents(allPagesAgents)
	if err != nil {
		return err
	}

	for _, agent := range allAgents {
		var state int = 0
		var id string

		if agent.Alive {
			state = 1
		}

		adminState := "down"
		if agent.AdminStateUp {
			adminState = "up"
		}

		id = agent.ID
		if id == "" {
			if id, err = exporter.ExporterConfig.UUIDGenFunc(); err != nil {
				return err
			}
		}

		ch <- prometheus.MustNewConstMetric(exporter.Metrics["agent_state"].Metric,
			prometheus.CounterValue, float64(state),
			id, agent.Host, agent.Binary, adminState,
			endpointOpts["network"].Region)
	}

	return nil
}

// ListNetworks : Count total number of instantiated Networks
func ListNetworks(exporter *BaseOpenStackExporter, ch chan<- prometheus.Metric) error {
	type NetworkWithProvider struct {
		networks.Network
		provider.NetworkProviderExt
	}

	var allNetworks []NetworkWithProvider

	allPagesNetworks, err := networks.List(exporter.Client, networks.ListOpts{}).AllPages()
	if err != nil {
		return err
	}

	err = networks.ExtractNetworksInto(allPagesNetworks, &allNetworks)
	if err != nil {
		return err
	}

	for _, network := range allNetworks {
		ch <- prometheus.MustNewConstMetric(exporter.Metrics["network"].Metric,
			prometheus.GaugeValue, 1,
			network.ID, network.Name, strconv.FormatBool(network.AdminStateUp), network.Status, network.TenantID, network.ProjectID,
			endpointOpts["network"].Region, network.NetworkProviderExt.NetworkType, network.NetworkProviderExt.PhysicalNetwork, network.NetworkProviderExt.SegmentationID)
	}

	ch <- prometheus.MustNewConstMetric(exporter.Metrics["networks"].Metric,
		prometheus.GaugeValue, float64(len(allNetworks)),
		endpointOpts["network"].Region)

	return nil
}

// ListSecGroups : count total number of instantiated Security Groups
func ListSecGroups(exporter *BaseOpenStackExporter, ch chan<- prometheus.Metric) error {
	var allSecurityGroups []groups.SecGroup

	allPagesSecurityGroups, err := groups.List(exporter.Client, groups.ListOpts{}).AllPages()
	if err != nil {
		return err
	}

	allSecurityGroups, err = groups.ExtractGroups(allPagesSecurityGroups)
	if err != nil {
		return err
	}
	ch <- prometheus.MustNewConstMetric(exporter.Metrics["security_groups"].Metric,
		prometheus.GaugeValue, float64(len(allSecurityGroups)),
		endpointOpts["network"].Region)

	return nil
}

// ListSubnets : count total number of instantiated Subnets
func ListSubnets(exporter *BaseOpenStackExporter, ch chan<- prometheus.Metric) error {
	var allSubnets []subnets.Subnet

	allPagesSubnets, err := subnets.List(exporter.Client, subnets.ListOpts{}).AllPages()
	if err != nil {
		return err
	}

	allSubnets, err = subnets.ExtractSubnets(allPagesSubnets)
	if err != nil {
		return err
	}
	ch <- prometheus.MustNewConstMetric(exporter.Metrics["subnets"].Metric,
		prometheus.GaugeValue, float64(len(allSubnets)),
		endpointOpts["network"].Region)

	return nil
}

// PortBinding represents a port which includes port bindings
type PortBinding struct {
	ports.Port
	portsbinding.PortsBindingExt
}

// ListPorts generates metrics about ports inside the OpenStack cloud
func ListPorts(exporter *BaseOpenStackExporter, ch chan<- prometheus.Metric) error {
	var allPorts []PortBinding

	allPagesPorts, err := ports.List(exporter.Client, ports.ListOpts{}).AllPages()
	if err != nil {
		return err
	}

	err = ports.ExtractPortsInto(allPagesPorts, &allPorts)
	if err != nil {
		return err
	}

	portsWithNoIP := float64(0)
	lbaasPortsInactive := float64(0)

	for _, port := range allPorts {
		if port.Status == "ACTIVE" && len(port.FixedIPs) == 0 {
			portsWithNoIP++
		}

		if port.DeviceOwner == "neutron:LOADBALANCERV2" && port.Status != "ACTIVE" {
			lbaasPortsInactive++
		}

		ch <- prometheus.MustNewConstMetric(exporter.Metrics["port"].Metric,
			prometheus.GaugeValue, 1,
			port.ID, port.NetworkID, port.MACAddress, port.DeviceOwner, port.Status, port.VIFType, strconv.FormatBool(port.AdminStateUp), port.DeviceID,
			endpointOpts["network"].Region)
	}

	// NOTE(mnaser): We should deprecate this and users can replace it by
	//               count(openstack_neutron_port)
	ch <- prometheus.MustNewConstMetric(exporter.Metrics["ports"].Metric,
		prometheus.GaugeValue, float64(len(allPorts)),
		endpointOpts["network"].Region)

	// NOTE(mnaser): We should deprecate this and users can replace it by:
	//               count(openstack_neutron_port{device_owner="neutron:LOADBALANCERV2",status!="ACTIVE"})
	ch <- prometheus.MustNewConstMetric(exporter.Metrics["ports_lb_not_active"].Metric,
		prometheus.GaugeValue, lbaasPortsInactive,
		endpointOpts["network"].Region)

	ch <- prometheus.MustNewConstMetric(exporter.Metrics["ports_no_ips"].Metric,
		prometheus.GaugeValue, portsWithNoIP,
		endpointOpts["network"].Region)

	return nil
}

// ListNetworkIPAvailabilities : count total number of used IPs per Network
func ListNetworkIPAvailabilities(exporter *BaseOpenStackExporter, ch chan<- prometheus.Metric) error {
	var allNetworkIPAvailabilities []networkipavailabilities.NetworkIPAvailability

	allPagesNetworkIPAvailabilities, err := networkipavailabilities.List(exporter.Client, networkipavailabilities.ListOpts{}).AllPages()
	if err != nil {
		return err
	}

	allNetworkIPAvailabilities, err = networkipavailabilities.ExtractNetworkIPAvailabilities(allPagesNetworkIPAvailabilities)
	if err != nil {
		return err
	}

	for _, NetworkIPAvailabilities := range allNetworkIPAvailabilities {
		projectID := NetworkIPAvailabilities.ProjectID
		if projectID == "" && NetworkIPAvailabilities.TenantID != "" {
			projectID = NetworkIPAvailabilities.TenantID
		}

		for _, SubnetIPAvailability := range NetworkIPAvailabilities.SubnetIPAvailabilities {
			totalIPs, err := strconv.ParseFloat(SubnetIPAvailability.TotalIPs, 64)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(exporter.Metrics["network_ip_availabilities_total"].Metric,
				prometheus.GaugeValue, totalIPs, NetworkIPAvailabilities.NetworkID,
				NetworkIPAvailabilities.NetworkName, strconv.Itoa(SubnetIPAvailability.IPVersion), SubnetIPAvailability.CIDR,
				SubnetIPAvailability.SubnetName, projectID,
				endpointOpts["network"].Region)

			usedIPs, err := strconv.ParseFloat(SubnetIPAvailability.UsedIPs, 64)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(exporter.Metrics["network_ip_availabilities_used"].Metric,
				prometheus.GaugeValue, usedIPs, NetworkIPAvailabilities.NetworkID,
				NetworkIPAvailabilities.NetworkName, strconv.Itoa(SubnetIPAvailability.IPVersion), SubnetIPAvailability.CIDR,
				SubnetIPAvailability.SubnetName, projectID,
				endpointOpts["network"].Region)
		}
	}

	return nil
}

// ListRouters : count total number of instantiated Routers and those that are not in ACTIVE state
func ListRouters(exporter *BaseOpenStackExporter, ch chan<- prometheus.Metric) error {
	var allRouters []routers.Router

	allPagesRouters, err := routers.List(exporter.Client, routers.ListOpts{}).AllPages()
	if err != nil {
		return err
	}

	allRouters, err = routers.ExtractRouters(allPagesRouters)
	if err != nil {
		return err
	}

	failedRouters := 0
	for _, router := range allRouters {
		if router.Status != "ACTIVE" {
			failedRouters = failedRouters + 1
		}
		allPagesL3Agents, err := routers.ListL3Agents(exporter.Client, router.ID).AllPages()
		if err != nil {
			return err
		}
		l3Agents, err := routers.ExtractL3Agents(allPagesL3Agents)
		if err != nil {
			return err
		}
		for _, agent := range l3Agents {
			var state int

			if agent.Alive {
				state = 1
			}

			ch <- prometheus.MustNewConstMetric(exporter.Metrics["l3_agent_of_router"].Metric,
				prometheus.GaugeValue, float64(state), router.ID, agent.ID,
				agent.HAState, strconv.FormatBool(agent.Alive), strconv.FormatBool(agent.AdminStateUp), agent.Host,
				endpointOpts["network"].Region)
		}
		ch <- prometheus.MustNewConstMetric(exporter.Metrics["router"].Metric,
			prometheus.GaugeValue, 1, router.ID, router.Name, router.ProjectID,
			strconv.FormatBool(router.AdminStateUp), router.Status, router.GatewayInfo.NetworkID,
			endpointOpts["network"].Region)
	}

	ch <- prometheus.MustNewConstMetric(exporter.Metrics["routers"].Metric,
		prometheus.GaugeValue, float64(len(allRouters)),
		endpointOpts["network"].Region)
	ch <- prometheus.MustNewConstMetric(exporter.Metrics["routers_not_active"].Metric,
		prometheus.GaugeValue, float64(failedRouters),
		endpointOpts["network"].Region)

	return nil
}
