package team

import (
	"fmt"
	"strings"

	"github.com/nickdu2009/learn-claude-code/pkg/tools"
)

type AgentFactory interface {
	Build(member Member) (systemPrompt string, registry *tools.Registry, err error)
}

type RegistryBuilder func(member Member) (*tools.Registry, error)

type RuntimeFactory struct {
	Workdir         string
	RegistryBuilder RegistryBuilder
}

func (f RuntimeFactory) Build(member Member) (string, *tools.Registry, error) {
	if strings.TrimSpace(f.Workdir) == "" {
		return "", nil, fmt.Errorf("workdir is required")
	}
	if f.RegistryBuilder == nil {
		return "", nil, fmt.Errorf("registry builder is not configured")
	}

	registry, err := f.RegistryBuilder(member)
	if err != nil {
		return "", nil, err
	}

	systemPrompt := fmt.Sprintf(
		"You are teammate %q with role %q at %s.\n"+
			"Prefer tools over prose.\n"+
			"You are part of a persistent agent team. Complete the assigned task, watch your inbox for updates, and send useful progress or results back to the lead.",
		member.Name,
		member.Role,
		f.Workdir,
	)

	return systemPrompt, registry, nil
}

