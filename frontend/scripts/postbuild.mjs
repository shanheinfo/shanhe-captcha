import { copyFileSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const src = resolve(here, '../types/index.d.ts')
const dst = resolve(here, '../dist/index.d.ts')

mkdirSync(dirname(dst), { recursive: true })
copyFileSync(src, dst)
console.log('types → dist/index.d.ts')
