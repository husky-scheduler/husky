package ipc

import "encoding/json"

type RequestType string

const (
	ReqPing   RequestType = "ping"
	ReqStatus RequestType = "status"
	ReqRun    RequestType = "run"
	ReqStop   RequestType = "stop"
	ReqDag    RequestType = "dag"
	ReqRetry  RequestType = "retry"
	ReqCancel RequestType = "cancel"
	ReqSkip   RequestType = "skip"
	ReqReload RequestType = "reload"
)

type Request struct {
	Type   RequestType `json:"type"`
	Job    string      `json:"job,omitempty"`
	Reason string      `json:"reason,omitempty"`
	Force  bool        `json:"force,omitempty"`
	JSON   bool        `json:"json,omitempty"`
}

type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type JobStatus struct {
	Name        string  `json:"name"`
	LastSuccess *string `json:"last_success,omitempty"`
	LastFailure *string `json:"last_failure,omitempty"`
	NextRun     *string `json:"next_run,omitempty"`
	Running     bool    `json:"running"`
	Paused      bool    `json:"paused,omitempty"`
}
