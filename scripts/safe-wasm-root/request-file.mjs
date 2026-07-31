import { mkdir, readFile, rename, writeFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'

const SCHEMA = 'safe-wasm-root-request/v1'

export function requestPathFromArgs() {
  const value = process.argv[2]?.trim() || process.env.REQUEST_FILE?.trim()
  if (!value) {
    throw new Error('Pass the Safe request JSON path as the first argument or REQUEST_FILE')
  }
  return resolve(value)
}

export function defaultRequestPath(safeTxHash) {
  return resolve(`safe-request-${safeTxHash}.json`)
}

export async function readRequest(path) {
  let parsed
  try {
    parsed = JSON.parse(await readFile(path, 'utf8'))
  } catch (error) {
    throw new Error(`Cannot read Safe request ${path}: ${error.message}`)
  }
  if (parsed.schema !== SCHEMA) {
    throw new Error(`Unsupported request schema ${parsed.schema || '<missing>'}`)
  }
  if (!parsed.transaction || !Array.isArray(parsed.signatures)) {
    throw new Error('Safe request is missing transaction or signatures')
  }
  return parsed
}

export async function createRequest(path, request) {
  await mkdir(dirname(path), { recursive: true })
  await writeFile(
    path,
    `${JSON.stringify({ schema: SCHEMA, ...request }, null, 2)}\n`,
    {
      encoding: 'utf8',
      flag: 'wx',
      mode: 0o600,
    },
  )
}

export async function updateRequest(path, request) {
  const temporaryPath = `${path}.${process.pid}.tmp`
  await writeFile(temporaryPath, `${JSON.stringify(request, null, 2)}\n`, {
    encoding: 'utf8',
    flag: 'wx',
    mode: 0o600,
  })
  await rename(temporaryPath, path)
}
