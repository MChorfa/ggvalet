package lab

// Domain models an authority or operational host domain in the delivery lab.
type Domain struct {
	Host        string   `json:"host"`
	Description string   `json:"description"`
	Projects    []string `json:"projects"`
	IsAirgap    bool     `json:"is_airgap"`
}

// Topology encapsulates the complete simulated multi-instance environment.
type Topology struct {
	Shared    Domain `json:"shared"`
	Dedicated Domain `json:"dedicated"`
	Airgap    Domain `json:"airgap"`
	Trustwall Domain `json:"trustwall"`
	Transfer  Domain `json:"transfer"`
}

// DefaultTopology returns the standard 5-domain simulated delivery world.
func DefaultTopology() Topology {
	return Topology{
		Shared: Domain{
			Host:        "gitlab-shared.local",
			Description: "Multi-tenant shared GitLab instance hosting varied team maturity levels",
			Projects: []string{
				"gitlab-shared/team-a", // Canonical ✓
				"gitlab-shared/team-b", // Adverse ✗
				"gitlab-shared/team-c", // Drifted ~
			},
			IsAirgap: false,
		},
		Dedicated: Domain{
			Host:        "gitlab-dedicated.local",
			Description: "Isolated, high-assurance controlled instance for sovereign delivery",
			Projects: []string{
				"gitlab-dedicated/controlled-proj",
			},
			IsAirgap: false,
		},
		Airgap: Domain{
			Host:        "gitlab-airgap.local",
			Description: "Destination air-gapped registry and repository beyond the transfer diode",
			Projects: []string{
				"gitlab-airgap/secure-dest",
			},
			IsAirgap: true,
		},
		Trustwall: Domain{
			Host:        "trustwall.local",
			Description: "Deterministic verification, admission, and quarantine gate",
			Projects:    nil,
			IsAirgap:    false,
		},
		Transfer: Domain{
			Host:        "transfer-station.local",
			Description: "Simulated hardware diode enforcing unidirectional air-gap bundle transfer",
			Projects:    nil,
			IsAirgap:    false,
		},
	}
}
