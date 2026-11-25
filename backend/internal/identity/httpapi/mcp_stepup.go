package httpapi

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// stepUpScope decides the step-up challenge (#457 slice B3, MCP 2025-11-25
// "Scope Challenge Handling"). It challenges only a single tools/call request
// with an id whose params name a registered tool, when the tool's scope is not
// held but the account's policy allows it. scope is then the held scopes plus
// the missing one, sorted, so a client that re-authorizes keeps what it had and
// asks only for what the policy allows.
//
// The decision is advisory: anything else passes through, and the scope
// middleware (requireToolScope) still decides every call. A name this parser
// reads differently from the SDK can only cost a challenge, never admit a call.
func (e *MCPEndpoint) stepUpScope(body []byte, access identity.MCPAccess) (string, bool) {
	name, ok := toolsCallName(body)
	if !ok {
		return "", false
	}
	scope, known := e.required[name]
	if !known || slices.Contains(access.Scopes, scope) || !slices.Contains(access.Allowed, scope) {
		return "", false
	}
	wanted := []string{scope}
	for _, held := range access.Scopes {
		if slices.Contains(access.Allowed, held) && !slices.Contains(wanted, held) {
			wanted = append(wanted, held)
		}
	}
	slices.Sort(wanted)
	return strings.Join(wanted, " "), true
}

// toolsCallName returns params.name of a single JSON-RPC tools/call request
// that carries an id. It decodes with the SDK's JSON-RPC decoder and reads the
// exact key "name"; a notification, another method, a non-object params or a
// non-string name is not a call to challenge.
func toolsCallName(body []byte) (string, bool) {
	msg, err := jsonrpc.DecodeMessage(body)
	if err != nil {
		return "", false
	}
	req, ok := msg.(*jsonrpc.Request)
	if !ok || !req.ID.IsValid() || req.Method != "tools/call" {
		return "", false
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(req.Params, &params) != nil {
		return "", false
	}
	var name string
	if json.Unmarshal(params["name"], &name) != nil || name == "" {
		return "", false
	}
	return name, true
}
