'use client'

import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { clearToken } from '@/lib/api'

const links = [
  { href: '/',        label: 'Фильмы'          },
  { href: '/series',  label: 'Сериалы'         },
  { href: '/upload',  label: 'Добавить фильм'  },
  { href: '/queue',   label: 'В работе'        },
  { href: '/search',  label: 'Поиск'           },
]

export function Nav() {
  const pathname = usePathname()
  const router = useRouter()

  function handleLogout() {
    clearToken()
    router.push('/login')
  }

  return (
    <nav className="border-b border-gray-800 bg-gray-900">
      <div className="px-3 py-3 sm:px-6">
        {/* Brand row */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-6">
            <span className="font-semibold text-white">Media Admin</span>
            {/* Desktop links */}
            <div className="hidden sm:flex items-center gap-6">
              {links.map(({ href, label }) => {
                const active = href === '/' ? pathname === '/' : pathname.startsWith(href)
                return (
                  <Link
                    key={href}
                    href={href}
                    className={`text-sm transition ${active ? 'text-indigo-400' : 'text-gray-400 hover:text-gray-200'}`}
                  >
                    {label}
                  </Link>
                )
              })}
            </div>
          </div>
          <button
            onClick={handleLogout}
            className="text-sm text-gray-400 hover:text-gray-200"
          >
            Выйти
          </button>
        </div>
        {/* Mobile links row */}
        <div className="flex items-center gap-5 mt-2.5 overflow-x-auto pb-0.5 sm:hidden">
          {links.map(({ href, label }) => {
            const active = href === '/' ? pathname === '/' : pathname.startsWith(href)
            return (
              <Link
                key={href}
                href={href}
                className={`shrink-0 text-sm transition ${active ? 'text-indigo-400' : 'text-gray-400 hover:text-gray-200'}`}
              >
                {label}
              </Link>
            )
          })}
        </div>
      </div>
    </nav>
  )
}
