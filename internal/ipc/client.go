package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client is a one-shot IPC client that connects to the huskyd Unix socket.
type Client struct {
	socketPath string
}

// NewClient returns a Client configured to connect to socketPath.
func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

// Do sends req to the daemon and returns the Response.
// A new connection is opened and closed for each call.
func (c *Client) Do(req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 3*time.Second)
	if err != nil {
		return Response{}, fmt.Errorf("ipc: connect %q: %w", c.socketPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("ipc: send: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("ipc: receive: %w", err)
	}
	return resp, nil
}
