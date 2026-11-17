package dto

// DeletedUsersResponse is the answer of DELETE /users: how many users were deleted, and how many of them held each
// role (a user with two roles counts under both; "none" for users without a role).
type DeletedUsersResponse struct {
	Deleted int            `json:"deleted"`
	ByRole  map[string]int `json:"by_role"`
}
