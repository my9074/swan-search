package swan

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	memberStatusUp   = 0
	memberStatusDown = 1
)

// the status of a member node
type memberStatus int

// cluster is a collection of swan nodes
type cluster struct {
	sync.RWMutex
	// a collection of nodes
	members []*member
	// the http client
	client *http.Client
}

// member represents an individual endpoint
type member struct {
	// the name / ip address of the host
	endpoint string
	// the status of the host
	status memberStatus
}

// newCluster returns a new swan cluster
func newCluster(client *http.Client, swanURL string) (*cluster, error) {
	// step: extract and basic validate the endpoints
	var members []*member
	var defaultProto string

	for _, endpoint := range strings.Split(swanURL, ",") {
		// step: check for nothing
		if endpoint == "" {
			return nil, errors.New("endpoint is blank")
		}
		// step: parse the url
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("endpoint: %s is invalid reason: %s", endpoint, err))
		}
		// step: set the default protocol schema
		if defaultProto == "" {
			if u.Scheme != "http" && u.Scheme != "https" {
				return nil, errors.New(fmt.Sprintf("endpoint: %s protocol must be (http|https)", endpoint))
			}
			defaultProto = u.Scheme
		}
		// step: does the url have a protocol schema? if not, use the default
		if u.Scheme == "" || u.Opaque != "" {
			urlWithScheme := fmt.Sprintf("%s://%s", defaultProto, u.String())
			if u, err = url.Parse(urlWithScheme); err != nil {
				panic(fmt.Sprintf("unexpected parsing error for URL '%s' with added default scheme: %s", urlWithScheme, err))
			}
		}

		// step: check for empty hosts
		if u.Host == "" {
			return nil, errors.New(fmt.Sprintf("endpoint: %s must have a host", endpoint))
		}

		// step: create a new node for this endpoint
		members = append(members, &member{endpoint: u.String()})
	}

	return &cluster{
		client:  client,
		members: members,
	}, nil
}

// retrieve the current member, i.e. the current endpoint in use
func (c *cluster) getMember() (string, error) {
	c.RLock()
	defer c.RUnlock()
	for _, n := range c.members {
		if n.status == memberStatusUp {
			return n.endpoint, nil
		}
	}

	return "", ErrSwanDown
}

// markDown marks down the current endpoint
func (c *cluster) markDown(endpoint string) {
	c.Lock()
	defer c.Unlock()
	for _, n := range c.members {
		// step: check if this is the node and it's marked as up - The double  checking on the
		// nodes status ensures the multiple calls don't create multiple checks
		if n.status == memberStatusUp && n.endpoint == endpoint {
			n.status = memberStatusDown
			go c.healthCheckNode(n)
			break
		}
	}
}

// healthCheckNode performs a health check on the node and when active updates the status
func (c *cluster) healthCheckNode(node *member) {
	// step: wait for the node to become active ... we are assuming a /ping is enough here
	for {
		res, err := c.client.Get(fmt.Sprintf("%s/%s", node.endpoint, swanAPIPing))
		if err == nil && res.StatusCode == 200 {
			break
		}
		<-time.After(time.Duration(5 * time.Second))
	}
	// step: mark the node as active again
	c.Lock()
	defer c.Unlock()
	node.status = memberStatusUp
}

// activeMembers returns a list of active members
func (c *cluster) activeMembers() []string {
	return c.membersList(memberStatusUp)
}

// nonActiveMembers returns a list of non-active members in the cluster
func (c *cluster) nonActiveMembers() []string {
	return c.membersList(memberStatusDown)
}

// memberList returns a list of members of a specified status
func (c *cluster) membersList(status memberStatus) []string {
	c.RLock()
	defer c.RUnlock()
	var list []string
	for _, m := range c.members {
		if m.status == status {
			list = append(list, m.endpoint)
		}
	}

	return list
}

// size returns the size of the cluster
func (c *cluster) size() int {
	return len(c.members)
}

// String returns a string representation
func (m member) String() string {
	status := "UP"
	if m.status == memberStatusDown {
		status = "DOWN"
	}

	return fmt.Sprintf("member: %s:%s", m.endpoint, status)
}
