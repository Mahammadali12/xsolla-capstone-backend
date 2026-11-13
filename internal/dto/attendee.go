package dto

// Attendee is a participant returned with an event or a 1:1 mentor slot.
type Attendee struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
