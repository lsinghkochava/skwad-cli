package models

import "github.com/google/uuid"

// PersonaType distinguishes system-shipped personas from user-created ones.
type PersonaType string

const (
	PersonaTypeSystem PersonaType = "system"
	PersonaTypeUser   PersonaType = "user"
)

// PersonaState tracks the lifecycle of a persona.
type PersonaState string

const (
	PersonaStateEnabled  PersonaState = "enabled"
	PersonaStateDisabled PersonaState = "disabled"
	PersonaStateDeleted  PersonaState = "deleted"
)

// Persona is a named system prompt modifier applied to an agent at launch.
type Persona struct {
	ID                uuid.UUID    `json:"id"`
	Name              string       `json:"name"`
	Instructions      string       `json:"instructions"`
	Type              PersonaType  `json:"type"`
	State             PersonaState `json:"state"`
	AllowedCategories []string     `json:"allowed_categories,omitempty"`
}

// Fixed UUIDs for system personas — must remain stable across installs.
var (
	PersonaIDKentBeck    = uuid.MustParse("a1000001-0000-0000-0000-000000000001")
	PersonaIDMartinFowler = uuid.MustParse("a1000001-0000-0000-0000-000000000002")
	PersonaIDLinusTorvalds = uuid.MustParse("a1000001-0000-0000-0000-000000000003")
	PersonaIDUncleBob    = uuid.MustParse("a1000001-0000-0000-0000-000000000004")
	PersonaIDJohnCarmack = uuid.MustParse("a1000001-0000-0000-0000-000000000005")
	PersonaIDDaveFarley  = uuid.MustParse("a1000001-0000-0000-0000-000000000006")
)

// DefaultPersonas returns the system-shipped persona set.
func DefaultPersonas() []Persona {
	return []Persona{
		{
			ID:    PersonaIDKentBeck,
			Name:  "Kent Beck",
			Type:  PersonaTypeSystem,
			State: PersonaStateEnabled,
			Instructions: "Follow TDD strictly. Write the simplest code that could possibly work. " +
				"Always red-green-refactor. Prefer small steps and fast feedback.",
		},
		{
			ID:    PersonaIDMartinFowler,
			Name:  "Martin Fowler",
			Type:  PersonaTypeSystem,
			State: PersonaStateEnabled,
			Instructions: "Prioritize readability and clean design. Apply design patterns judiciously. " +
				"Continuously refactor. Prefer intention-revealing names.",
		},
		{
			ID:    PersonaIDLinusTorvalds,
			Name:  "Linus Torvalds",
			Type:  PersonaTypeSystem,
			State: PersonaStateEnabled,
			Instructions: "Value simplicity and performance above all else. Be pragmatic. " +
				"Avoid over-engineering. Write code that the hardware can execute efficiently.",
		},
		{
			ID:    PersonaIDUncleBob,
			Name:  "Uncle Bob",
			Type:  PersonaTypeSystem,
			State: PersonaStateEnabled,
			Instructions: "Apply SOLID principles. Write clean, self-documenting code. " +
				"Keep functions small and do one thing. Separate concerns rigorously.",
		},
		{
			ID:    PersonaIDJohnCarmack,
			Name:  "John Carmack",
			Type:  PersonaTypeSystem,
			State: PersonaStateEnabled,
			Instructions: "Focus on deep technical correctness and performance. " +
				"Prefer linear, readable code over abstraction. Be aware of hardware and memory.",
		},
		{
			ID:    PersonaIDDaveFarley,
			Name:  "Dave Farley",
			Type:  PersonaTypeSystem,
			State: PersonaStateEnabled,
			Instructions: "Embrace continuous delivery. Write code that is always releasable. " +
				"Test everything. Take small incremental steps. Automate ruthlessly.",
		},
	}
}
