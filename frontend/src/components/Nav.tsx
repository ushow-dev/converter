'use client'

import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { clearToken } from '@/lib/api'

const links = [
  { href: '/',       label: 'Фильмы'   },
  { href: '/queue',  label: 'В работе' },
  { href: '/search', label: 'Поиск'   },
]

export function Nav() {
  const pathname = usePathname()
  const router = useRouter()

  function handleLogout() {
    clearToken()
    router.push('/login')
  }

  return (
    <nav className="border-b border-gray-800 bg-gray-900 px-6 py-3">
      <div className="mx-auto flex max-w-7xl items-center justify-between">
        <div className="flex items-center gap-6">
          <span className="font-semibold text-white">Media Admin</span>
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
        <button
          onClick={handleLogout}
          className="text-sm text-gray-400 hover:text-gray-200"
        >
          Выйти
        </button>
      </div>
    </nav>
  )
}
