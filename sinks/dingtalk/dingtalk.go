// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dingtalk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AliyunContainerService/kube-eventer/core"
	"k8s.io/api/core/v1"
	"k8s.io/klog"
)

const (
	DINGTALK_SINK         = "DingTalkSink"
	WARNING           int = 2
	NORMAL            int = 1
	DEFAULT_MSG_TYPE      = "text"
	CONTENT_TYPE_JSON     = "application/json"
	LABE_TEMPLATE         = "%s\n"
)

var (
	MSG_TEMPLATE = "Level:%s \nKind:%s \nNamespace:%s \nName:%s \nReason:%s \nTimestamp:%s \nMessage:%s"

	MSG_TEMPLATE_ARR = [][]string{
		{"Level"},
		{"Kind"},
		{"Namespace"},
		{"Name"},
		{"Reason"},
		{"Timestamp"},
		{"Message"},
	}
)

/**
dingtalk msg struct
*/
type DingTalkMsg struct {
	MsgType string       `json:"msgtype"`
	Text    DingTalkText `json:"text"`
}

type DingTalkText struct {
	Content string `json:"content"`
}

/**
dingtalk sink usage
--sink:dingtalk:https://oapi.dingtalk.com/robot/send?access_token=[access_token]&level=Warning&label=[label]

level: Normal or Warning. The event level greater than global level will emit.
label: some thing unique when you want to distinguish different k8s clusters.
*/
type DingTalkSink struct {
	Endpoint   string
	Namespaces []string
	Kinds      []string
	Token      string
	Level      int
	Labels     []string
}

func (d *DingTalkSink) Name() string {
	return DINGTALK_SINK
}

func (d *DingTalkSink) Stop() {
	//do nothing
}

func (d *DingTalkSink) ExportEvents(batch *core.EventBatch) {
	for _, event := range batch.Events {
		if d.isEventLevelDangerous(event.Type) {
			d.Ding(event)
			// add threshold
			time.Sleep(time.Millisecond * 50)
		}
	}
}

func (d *DingTalkSink) isEventLevelDangerous(level string) bool {
	score := getLevel(level)
	if score >= d.Level {
		return true
	}
	return false
}

func (d *DingTalkSink) Ding(event *v1.Event) {
	if d.Namespaces != nil {
		skip := true
		for _, namespace := range d.Namespaces {
			if namespace == event.Namespace {
				skip = false
				break
			}
		}
		if skip {
			return
		}
	}

	if d.Kinds != nil {
		skip := true
		for _, kind := range d.Kinds {
			if kind == event.InvolvedObject.Kind {
				skip = false
				break
			}
		}
		if skip {
			return
		}
	}

	msg := createMsgFromEvent(d.Labels, event)
	if msg == nil {
		klog.Warningf("failed to create msg from event,because of %v", event)
		return
	}

	msg_bytes, err := json.Marshal(msg)
	if err != nil {
		klog.Warningf("failed to marshal msg %v", msg)
		return
	}

	b := bytes.NewBuffer(msg_bytes)

	resp, err := http.Post(fmt.Sprintf("https://%s?access_token=%s", d.Endpoint, d.Token), CONTENT_TYPE_JSON, b)
	if err != nil || resp.StatusCode != http.StatusOK {
		klog.Errorf("failed to send msg to dingtalk,because of %s resp code is %d", err.Error(), resp.StatusCode)
		return
	}
}

func getLevel(level string) int {
	score := 0
	switch level {
	case v1.EventTypeWarning:
		score += 2
	case v1.EventTypeNormal:
		score += 1
	default:
		//score will remain 0
	}
	return score
}

func createMsgFromEvent(labels []string, event *v1.Event) *DingTalkMsg {
	msg := &DingTalkMsg{}
	msg.MsgType = DEFAULT_MSG_TYPE
	template := MSG_TEMPLATE
	if len(labels) > 0 {
		for _, label := range labels {
			template = fmt.Sprintf(LABE_TEMPLATE, label) + template
		}
	}
	msg.Text = DingTalkText{
		Content: fmt.Sprintf(template, event.Type, event.InvolvedObject.Kind, event.Namespace, event.Name, event.Reason, event.LastTimestamp.String(), event.Message),
	}
	return msg
}

//func drawEventTableText(labels []string, event *v1.Event) string {
//	tableString := &strings.Builder{}
//	table := tablewriter.NewWriter(tableString)
//
//	data := make([][]string, 7)
//
//	for index, _ := range MSG_TEMPLATE_ARR {
//		data[index] = append(data[index], MSG_TEMPLATE_ARR[index]...)
//	}
//
//	data[0] = append(data[0], event.Type)
//	data[1] = append(data[1], event.InvolvedObject.Kind)
//	data[2] = append(data[2], event.Namespace)
//	data[3] = append(data[3], event.Name)
//	data[4] = append(data[4], event.Reason)
//	data[5] = append(data[5], event.LastTimestamp.String())
//	data[6] = append(data[6], event.Message)
//
//	for _, v := range data {
//		table.Append(v)
//	}
//	table.SetHeader([]string{"", strings.Join(labels, ",")})
//
//	table.Render() // Send output
//
//	return tableString.String()
//}

func NewDingTalkSink(uri *url.URL) (*DingTalkSink, error) {
	d := &DingTalkSink{
		Level: WARNING,
	}
	if len(uri.Host) > 0 {
		d.Endpoint = uri.Host + uri.Path
	}
	opts := uri.Query()

	if len(opts["access_token"]) >= 1 {
		d.Token = opts["access_token"][0]
	} else {
		return nil, fmt.Errorf("you must provide dingtalk bot access_token")
	}

	if len(opts["level"]) >= 1 {
		d.Level = getLevel(opts["level"][0])
	}

	//add extra labels
	if len(opts["label"]) >= 1 {
		d.Labels = opts["label"]
	}

	d.Namespaces = getValues(opts["namespaces"])
	// kinds:https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#lists-and-simple-kinds
	// such as node,pod,component and so on
	d.Kinds = getValues(opts["kinds"])

	return d, nil
}

func getValues(o []string) []string {
	if len(o) >= 1 {
		if len(o[0]) == 0 {
			return nil
		}
	}
	return strings.Split(o[0], ",")
}
