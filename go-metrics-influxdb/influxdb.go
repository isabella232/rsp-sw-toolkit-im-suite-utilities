/* Apache v2 license
*  Copyright (C) <2019> Intel Corporation
*
*  SPDX-License-Identifier: Apache-2.0
 */

package influxdb

import (
	"fmt"
	"log"
	uurl "net/url"
	"time"

	"github.com/influxdata/influxdb/client"
	"github.com/intel/rsp-sw-toolkit-im-suite-utilities/go-metrics"
)

type reporter struct {
	reg      metrics.Registry
	interval time.Duration

	url      uurl.URL
	database string
	username string
	password string
	tags     map[string]string

	client *client.Client
}

// InfluxDB starts a InfluxDB reporter which will post the metrics from the given registry at each d interval.
func InfluxDB(r metrics.Registry, d time.Duration, url, database, username, password string) {
	InfluxDBWithTags(r, d, url, database, username, password, nil)
}

// InfluxDBWithTags starts a InfluxDB reporter which will post the metrics from the given registry at each d interval with the specified tags
func InfluxDBWithTags(r metrics.Registry, d time.Duration, url, database, username, password string, tags map[string]string) {
	u, err := uurl.Parse(url)
	if err != nil {
		log.Printf("unable to parse InfluxDB url %s. err=%v", url, err)
		return
	}

	rep := &reporter{
		reg:      r,
		interval: d,
		url:      *u,
		database: database,
		username: username,
		password: password,
		tags:     tags,
	}
	if err := rep.makeClient(); err != nil {
		log.Printf("unable to make InfluxDB client. err=%v", err)
		return
	}

	rep.run()
}

func (r *reporter) makeClient() (err error) {
	r.client, err = client.NewClient(client.Config{
		URL:      r.url,
		Username: r.username,
		Password: r.password,
	})

	return
}

func (r *reporter) run() {
	intervalTicker := time.Tick(r.interval)
	pingTicker := time.Tick(time.Second * 5)

	for {
		select {
		case <-intervalTicker:
			if err := r.send(); err != nil {
				log.Printf("unable to send metrics to InfluxDB. err=%v", err)
			}
		case <-pingTicker:
			_, _, err := r.client.Ping()
			if err != nil {
				log.Printf("got error while sending a ping to InfluxDB, trying to recreate client. err=%v", err)

				if err = r.makeClient(); err != nil {
					log.Printf("unable to make InfluxDB client. err=%v", err)
				}
			}
		}
	}
}

func (r *reporter) send() error {
	var pts []client.Point

	r.reg.Each(func(name string, i interface{}) {
		now := time.Now()

		switch metric := i.(type) {
		case metrics.Counter:
			ms := metric.Snapshot()
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.count", name),
				Tags:        r.tags,
				Fields: map[string]interface{}{
					"value": ms.Count(),
				},
				Time: now,
			})

		case metrics.Gauge:
			ms := metric.Snapshot()
			if ms.IsSet() {
				// Add the gauge's tag (if it has one) to the main tags list for sending for this data point
				tags := copyTagMap(r.tags)
				if ms.Tag() != nil {
					tag := *ms.Tag()
					tags[tag.Name] = tag.Value
				}

				pts = append(pts, client.Point{
					Measurement: fmt.Sprintf("%s.gauge", name),
					Tags:        tags,
					Fields: map[string]interface{}{
						"value": ms.Value(),
					},
					Time: now,
				})
				metric.Clear()
			}

		case metrics.GaugeFloat64:
			ms := metric.Snapshot()
			if ms.IsSet() {
				// Add the gauge's tag (if it has one) to the main tags list for sending for this data point
				tags := copyTagMap(r.tags)
				if ms.Tag() != nil {
					tag := *ms.Tag()
					tags[tag.Name] = tag.Value
				}

				pts = append(pts, client.Point{
					Measurement: fmt.Sprintf("%s.gauge", name),
					Tags:        tags,
					Fields: map[string]interface{}{
						"value": ms.Value(),
					},
					Time: now,
				})
				metric.Clear()
			}

		case metrics.GaugeCollection:
			ms := metric.Snapshot()
			if ms.IsSet() {
				for _, reading := range ms.Readings() {
					// Add the gauge's tag (if it has one) to the main tags list for sending for this data point
					tags := copyTagMap(r.tags)
					if reading.Tag != nil {
						tag := *reading.Tag
						tags[tag.Name] = tag.Value
					}

					pts = append(pts, client.Point{
						Measurement: fmt.Sprintf("%s.gauge", name), // individual points remain as a "gauge"
						Tags:        tags,
						Fields: map[string]interface{}{
							"value": reading.Reading,
						},
						Time: reading.Time,
					})
				}
				metric.Clear()
			}

		case metrics.Histogram:
			ms := metric.Snapshot()
			ps := ms.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.histogram", name),
				Tags:        r.tags,
				Fields: map[string]interface{}{
					"count":    ms.Count(),
					"max":      ms.Max(),
					"mean":     ms.Mean(),
					"min":      ms.Min(),
					"stddev":   ms.StdDev(),
					"variance": ms.Variance(),
					"p50":      ps[0],
					"p75":      ps[1],
					"p95":      ps[2],
					"p99":      ps[3],
					"p999":     ps[4],
					"p9999":    ps[5],
				},
				Time: now,
			})
		case metrics.Meter:
			ms := metric.Snapshot()
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.meter", name),
				Tags:        r.tags,
				Fields: map[string]interface{}{
					"count": ms.Count(),
					"m1":    ms.Rate1(),
					"m5":    ms.Rate5(),
					"m15":   ms.Rate15(),
					"mean":  ms.RateMean(),
				},
				Time: now,
			})
		case metrics.Timer:
			ms := metric.Snapshot()
			ps := ms.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.timer", name),
				Tags:        r.tags,
				Fields: map[string]interface{}{
					"count":    ms.Count(),
					"max":      ms.Max(),
					"mean":     ms.Mean(),
					"min":      ms.Min(),
					"stddev":   ms.StdDev(),
					"variance": ms.Variance(),
					"p50":      ps[0],
					"p75":      ps[1],
					"p95":      ps[2],
					"p99":      ps[3],
					"p999":     ps[4],
					"p9999":    ps[5],
					"m1":       ms.Rate1(),
					"m5":       ms.Rate5(),
					"m15":      ms.Rate15(),
					"meanrate": ms.RateMean(),
				},
				Time: now,
			})
		}
	})

	bps := client.BatchPoints{
		Points:   pts,
		Database: r.database,
	}

	_, err := r.client.Write(bps)
	return err
}

func copyTagMap(target map[string]string) map[string]string {
	copy := make(map[string]string)
	for name, value := range target {
		copy[name] = value
	}

	return copy
}
