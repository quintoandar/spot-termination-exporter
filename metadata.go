package main

import (
	"encoding/json"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"time"
)

type terminationCollector struct {
	metadataEndpoint     string
	terminationIndicator *prometheus.Desc
	terminationTime      *prometheus.Desc
}

type InstanceAction struct {
	Action string    `json:"action"`
	Time   time.Time `json:"time"`
}

var (
	labels = []string {
		"availability_zone",
		"hostname",
		"instance_id",
		"instance_type",
	}
)

func (c *terminationCollector) getMetadata(path string) (string, error) {
	timeout := time.Duration(1 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	url := c.metadataEndpoint + path
	idResp, err := client.Get(url)
	if err != nil {
		log.Errorf("error request metadata from %s: %s", url, err.Error())
		return "", err
	}
	if idResp.StatusCode == 404 {
		log.Errorf("endpoint %s not found", url)
		return "", nil
	}
	defer idResp.Body.Close()
	value, _ := ioutil.ReadAll(idResp.Body)

	return string(value), nil

}

func NewTerminationCollector(me string) *terminationCollector {
	return &terminationCollector{
		metadataEndpoint:     me,
		terminationIndicator: prometheus.NewDesc("aws_instance_termination_imminent", "Instance is about to be terminated", append(labels, "instance_action"), nil),
		terminationTime:      prometheus.NewDesc("aws_instance_termination_in", "Instance will be terminated in", labels, nil),
	}
}

func (c *terminationCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.terminationIndicator
	ch <- c.terminationTime

}

func (c *terminationCollector) Collect(ch chan<- prometheus.Metric) {
	log.Info("Fetching termination data from metadata-service")

	az, _ := c.getMetadata("placement/availability-zone")
	hostname, _ := c.getMetadata("hostname")
	instanceId, _ := c.getMetadata("instance-id")
	instanceType, _ := c.getMetadata("instance-type")

	action, err := c.getMetadata("spot/instance-action")
	if err != nil {
		log.Errorf("Failed to fetch data from metadata service: %s", err)
		return
	} else {

		if action == "" {
			log.Debug("instance-action endpoint not found")
			ch <- prometheus.MustNewConstMetric(c.terminationIndicator, prometheus.GaugeValue, 0, az, hostname, instanceId, instanceType, action)
			return
		} else {

			var ia = InstanceAction{}
			err = json.Unmarshal([]byte(action), &ia)

			// value may be present but not be a time according to AWS docs,
			// so parse error is not fatal
			if err != nil {
				log.Errorf("Couldn't parse instance-action metadata: %s", err)
				ch <- prometheus.MustNewConstMetric(c.terminationIndicator, prometheus.GaugeValue, 0, az, hostname, instanceId, instanceType, "")
			} else {
				log.Infof("instance-action endpoint available, termination time: %v", ia.Time)
				ch <- prometheus.MustNewConstMetric(c.terminationIndicator, prometheus.GaugeValue, 1, az, hostname, instanceId, instanceType, ia.Action)
				delta := ia.Time.Sub(time.Now())
				if delta.Seconds() > 0 {
					ch <- prometheus.MustNewConstMetric(c.terminationTime, prometheus.GaugeValue, delta.Seconds(), az, hostname, instanceId, instanceType)
				}
			}
		}
	}
}
