package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/http/middlewares"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Users struct{ svc *service.Services }

func NewUsers(s *service.Services) *Users { return &Users{svc: s} }

// ----- Shared body shapes -----

type userUpdateBody struct {
	Email    string `json:"email" required:"true" format:"email" doc:"User email"`
	Username string `json:"username" required:"true" minLength:"1" doc:"Display username"`
	IsActive bool   `json:"isActive" doc:"Whether the account is active"`
}

type userPasswordBody struct {
	Password string `json:"password" required:"true" minLength:"8" doc:"New password (min 8 chars)"`
}

// userIDFromCtx pulls the caller's user id from the request context, annotating
// any auth failure with the caller's op. It delegates to
// middlewares.UserIDFromContext (the shared implementation the authz gate also
// uses); the op wrapper is what the profile endpoints want for their logs.
func userIDFromCtx(ctx context.Context, op string) (uuid.UUID, error) {
	id, err := middlewares.UserIDFromContext(ctx)
	if err != nil {
		return uuid.Nil, apperrors.WithOp(err, op)
	}
	return id, nil
}

// ----- List -----

type UsersListInput struct{}

type UsersListOutput struct {
	Body []model.User
}

func (h *Users) List(ctx context.Context, _ *UsersListInput) (*UsersListOutput, error) {
	users, err := h.svc.Users.List(ctx)
	if err != nil {
		return nil, err
	}
	return &UsersListOutput{Body: users}, nil
}

// ----- Get -----

type UsersGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"User ID"`
}

type UsersGetOutput struct {
	Body model.User
}

func (h *Users) Get(ctx context.Context, input *UsersGetInput) (*UsersGetOutput, error) {
	user, err := h.svc.Users.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &UsersGetOutput{Body: user}, nil
}

// ----- Update -----

type UsersUpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"User ID"`
	Body userUpdateBody
}

type UsersUpdateOutput struct {
	Body model.User
}

func (h *Users) Update(ctx context.Context, input *UsersUpdateInput) (*UsersUpdateOutput, error) {
	user, err := h.svc.Users.Update(ctx, input.ID, input.Body.Email, input.Body.Username, input.Body.IsActive)
	if err != nil {
		return nil, err
	}
	return &UsersUpdateOutput{Body: user}, nil
}

// ----- Delete -----

type UsersDeleteInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"User ID"`
}

type UsersDeleteOutput struct{}

func (h *Users) Delete(ctx context.Context, input *UsersDeleteInput) (*UsersDeleteOutput, error) {
	if err := h.svc.Users.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &UsersDeleteOutput{}, nil
}

// ----- UpdatePassword -----

type UsersUpdatePasswordInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"User ID"`
	Body userPasswordBody
}

type UsersUpdatePasswordOutput struct{}

func (h *Users) UpdatePassword(ctx context.Context, input *UsersUpdatePasswordInput) (*UsersUpdatePasswordOutput, error) {
	if err := h.svc.Users.UpdatePassword(ctx, input.ID, input.Body.Password); err != nil {
		return nil, err
	}
	return &UsersUpdatePasswordOutput{}, nil
}

// ----- AssignRole -----

type UsersAssignRoleBody struct {
	Role string `json:"role" required:"true" minLength:"1" doc:"Role name"`
}

type UsersAssignRoleInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"User ID"`
	Body UsersAssignRoleBody
}

type UsersAssignRoleOutput struct{}

func (h *Users) AssignRole(ctx context.Context, input *UsersAssignRoleInput) (*UsersAssignRoleOutput, error) {
	if err := h.svc.Users.AssignRole(ctx, input.ID, input.Body.Role); err != nil {
		return nil, err
	}
	return &UsersAssignRoleOutput{}, nil
}

// ----- GetProfile -----

type UsersGetProfileInput struct{}

type UsersGetProfileOutput struct {
	Body model.User
}

func (h *Users) GetProfile(ctx context.Context, _ *UsersGetProfileInput) (*UsersGetProfileOutput, error) {
	userID, err := userIDFromCtx(ctx, "UsersHandler.GetProfile")
	if err != nil {
		return nil, err
	}
	user, err := h.svc.Users.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &UsersGetProfileOutput{Body: user}, nil
}

// ----- UpdateProfile -----

type UsersUpdateProfileInput struct {
	Body userUpdateBody
}

type UsersUpdateProfileOutput struct {
	Body model.User
}

func (h *Users) UpdateProfile(ctx context.Context, input *UsersUpdateProfileInput) (*UsersUpdateProfileOutput, error) {
	userID, err := userIDFromCtx(ctx, "UsersHandler.UpdateProfile")
	if err != nil {
		return nil, err
	}

	// Users cannot change their own active status via profile.
	currentUser, err := h.svc.Users.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	user, err := h.svc.Users.Update(ctx, userID, input.Body.Email, input.Body.Username, currentUser.IsActive)
	if err != nil {
		return nil, err
	}
	return &UsersUpdateProfileOutput{Body: user}, nil
}

// ----- UpdateProfilePassword -----

type UsersUpdateProfilePasswordInput struct {
	Body userPasswordBody
}

type UsersUpdateProfilePasswordOutput struct{}

func (h *Users) UpdateProfilePassword(ctx context.Context, input *UsersUpdateProfilePasswordInput) (*UsersUpdateProfilePasswordOutput, error) {
	userID, err := userIDFromCtx(ctx, "UsersHandler.UpdateProfilePassword")
	if err != nil {
		return nil, err
	}
	if err := h.svc.Users.UpdatePassword(ctx, userID, input.Body.Password); err != nil {
		return nil, err
	}
	return &UsersUpdateProfilePasswordOutput{}, nil
}

// ----- Register -----

func (h *Users) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "users-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/users",
		Summary:     "List users",
		Tags:        []string{"users"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "users-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{id}",
		Summary:     "Get user",
		Tags:        []string{"users"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "users-update",
		Method:      http.MethodPut,
		Path:        "/api/v1/users/{id}",
		Summary:     "Update user",
		Tags:        []string{"users"},
		Errors:      errsUpsert,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID:   "users-delete",
		Method:        http.MethodDelete,
		Path:          "/api/v1/users/{id}",
		Summary:       "Delete user",
		Tags:          []string{"users"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsDelete,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID:   "users-update-password",
		Method:        http.MethodPut,
		Path:          "/api/v1/users/{id}/password",
		Summary:       "Update user password",
		Tags:          []string{"users"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsUpsert,
	}, h.UpdatePassword)

	huma.Register(api, huma.Operation{
		OperationID:   "users-assign-role",
		Method:        http.MethodPut,
		Path:          "/api/v1/users/{id}/role",
		Summary:       "Assign role to user",
		Tags:          []string{"users"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsUpsert,
	}, h.AssignRole)

	huma.Register(api, huma.Operation{
		OperationID: "users-get-profile",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/profile",
		Summary:     "Get current user profile",
		Tags:        []string{"auth"},
		Errors:      errsRead,
	}, h.GetProfile)

	huma.Register(api, huma.Operation{
		OperationID: "users-update-profile",
		Method:      http.MethodPut,
		Path:        "/api/v1/auth/profile",
		Summary:     "Update current user profile",
		Tags:        []string{"auth"},
		Errors:      errsUpsert,
	}, h.UpdateProfile)

	huma.Register(api, huma.Operation{
		OperationID:   "users-update-profile-password",
		Method:        http.MethodPut,
		Path:          "/api/v1/auth/profile/password",
		Summary:       "Update current user password",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsWrite,
	}, h.UpdateProfilePassword)
}
