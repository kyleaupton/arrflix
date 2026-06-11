import { PrimeIcons } from '@/icons'
import type { MenuItem } from 'primevue/menuitem'

export interface NavigationItem extends MenuItem {
  key: string
  label: string
  icon?: string
  route?: string
  items?: NavigationItem[]
}

export const navigationItems: NavigationItem[] = [
  {
    key: 'home',
    label: 'Home',
    icon: PrimeIcons.HOME,
    route: '/',
  },
  {
    key: 'library',
    label: 'Library',
    icon: PrimeIcons.VIDEO,
    route: '/library',
  },
  {
    key: 'downloads',
    label: 'Downloads',
    icon: PrimeIcons.DOWNLOAD,
    route: '/downloads',
  },
  {
    key: 'requests',
    label: 'Requests',
    icon: PrimeIcons.CLOCK,
    route: '/requests',
  },
  {
    key: 'users',
    label: 'Users',
    icon: PrimeIcons.USERS,
    route: '/users',
  },
  {
    key: 'settings',
    label: 'Settings',
    icon: PrimeIcons.COG,
    route: '/settings',
    // items: [
    //   {
    //     key: 'settings-general',
    //     label: 'General',
    //     icon: PrimeIcons.COG,
    //     route: '/settings/general',
    //   },
    //   {
    //     key: 'settings-routing',
    //     label: 'Routing',
    //     icon: PrimeIcons.SLIDERS_H,
    //     route: '/settings/routing',
    //   },
    //   {
    //     key: 'settings-libraries',
    //     label: 'Libraries',
    //     icon: PrimeIcons.FOLDER,
    //     route: '/settings/libraries',
    //   },
    //   {
    //     key: 'settings-indexers',
    //     label: 'Indexers',
    //     icon: PrimeIcons.SEARCH,
    //     route: '/settings/indexers',
    //   },
    //   {
    //     key: 'settings-downloaders',
    //     label: 'Downloaders',
    //     icon: PrimeIcons.DOWNLOAD,
    //     route: '/settings/downloaders',
    //   },
    //   {
    //     key: 'settings-name-templates',
    //     label: 'Name Templates',
    //     icon: PrimeIcons.FILE_EDIT,
    //     route: '/settings/name-templates',
    //   },
    // ],
  },
]
