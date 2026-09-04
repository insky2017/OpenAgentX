import fs from 'node:fs'

const source = fs.readFileSync(new URL('../src/main.jsx', import.meta.url), 'utf8')
const requiredMarkers = [
  "addEventListener('beforeinstallprompt'",
  "addEventListener('appinstalled'",
  'deferredInstallPrompt',
  'prompt.userChoice',
  'navigator.standalone',
]

for (const marker of requiredMarkers) {
  if (!source.includes(marker)) throw new Error(`missing PWA install marker: ${marker}`)
}
console.log('PWA install source assertions passed')
