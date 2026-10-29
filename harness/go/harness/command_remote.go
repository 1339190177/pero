package harness

import (
	"context"
	"fmt"
)

const (
	CommandListMethod    = "commands/list"
	CommandExecuteMethod = "commands/execute"
)

type CommandRemoteExecution struct {
	Matched   bool              `json:"matched"`
	Execution *CommandExecution `json:"execution,omitempty"`
}

// CommandRemoteAdapter exposes handler-free discovery and direct execution by
// durable Agent identity. Transport adapters can retain the same vocabulary
// without exposing registry internals or handler functions.
type CommandRemoteAdapter struct {
	agents   *AgentManager
	commands *CommandRegistry
}

func NewCommandRemoteAdapter(agents *AgentManager, commands *CommandRegistry) (*CommandRemoteAdapter, error) {
	if agents == nil || commands == nil {
		return nil, fmt.Errorf("harness: command remote requires agents and commands")
	}
	return &CommandRemoteAdapter{agents: agents, commands: commands}, nil
}

func (r *CommandRemoteAdapter) List(agentID SessionID) ([]CommandDescriptor, error) {
	agent, ok := r.agents.Get(agentID)
	if !ok {
		return nil, fmt.Errorf("harness: command remote agent %q not found", agentID)
	}
	return r.commands.ListForAgent(context.Background(), agent)
}

func (r *CommandRemoteAdapter) Execute(ctx context.Context, agentID SessionID, line string, images ...EncodedImageAttachment) (CommandRemoteExecution, error) {
	agent, ok := r.agents.Get(agentID)
	if !ok {
		return CommandRemoteExecution{}, fmt.Errorf("harness: command remote agent %q not found", agentID)
	}
	execution, matched, err := r.commands.Execute(ctx, agent, line, images...)
	if err != nil {
		return CommandRemoteExecution{Matched: matched}, err
	}
	if !matched {
		return CommandRemoteExecution{}, nil
	}
	detached := execution
	return CommandRemoteExecution{Matched: true, Execution: &detached}, nil
}
