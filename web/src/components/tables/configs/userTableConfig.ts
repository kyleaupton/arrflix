import { type TableColumn, type TableAction } from '../types'

// Define user type based on API response structure
export interface User {
  id: string
  email: string | null
  username: string | null
  isActive: boolean
  roles?: any // JSONB from database
  createdAt: string
  updatedAt: string
}

export const userColumns: TableColumn<User>[] = [
  {
    key: 'email',
    label: 'Email',
    sortable: true,
    filterable: true,
  },
  {
    key: 'username',
    label: 'Username',
    sortable: true,
    filterable: true,
  },
  {
    key: 'roles',
    label: 'Role',
    sortable: false,
    width: '150px',
    render: (value: any) => {
      // Parse roles from JSONB array
      let roleName = 'none'
      if (value) {
        try {
          const roles = typeof value === 'string' ? JSON.parse(value) : value
          if (Array.isArray(roles) && roles.length > 0) {
            roleName = roles[0].name || 'none'
          }
        } catch {
          roleName = 'none'
        }
      }

      const colorMap: Record<string, string> = {
        admin: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200',
        manager:
          'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200',
        user: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200',
        guest: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300',
      }
      const colorClass = colorMap[roleName] || colorMap.guest
      return `<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${colorClass}">${roleName}</span>`
    },
  },
  {
    key: 'isActive',
    label: 'Status',
    sortable: true,
    width: '120px',
    align: 'center',
    render: (value: boolean) => {
      return value
        ? '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">Active</span>'
        : '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300">Inactive</span>'
    },
  },
  {
    key: 'createdAt',
    label: 'Created',
    sortable: true,
    width: '150px',
    render: (value: string) => {
      return new Date(value).toLocaleDateString()
    },
  },
]

export const createUserActions = (
  onEdit: (user: User) => void,
  onDelete: (user: User) => void,
): TableAction<User>[] => [
  {
    key: 'edit',
    label: 'Edit',
    severity: 'primary',
    variant: 'text',
    command: onEdit,
  },
  {
    key: 'delete',
    label: 'Delete',
    severity: 'danger',
    variant: 'text',
    command: onDelete,
  },
]
