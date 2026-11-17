import type { InvitesCreateBody, UsersAssignRoleBody } from '@/client/types.gen'

export type AssignableRole = NonNullable<InvitesCreateBody['role']> & UsersAssignRoleBody['role']

/**
 * The roles a user can be invited with or assigned. The API's role catalog also
 * holds co_admin and viewer, which are never offered, so pickers list these
 * rather than the roles endpoint.
 */
export const ASSIGNABLE_ROLES: readonly AssignableRole[] = ['requester', 'admin']

export function isAssignableRole(name: unknown): name is AssignableRole {
  return ASSIGNABLE_ROLES.includes(name as AssignableRole)
}
