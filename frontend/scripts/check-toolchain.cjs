const { execFileSync } = require('child_process')

const nodeVersion = process.versions.node
const npmCommand = process.platform === 'win32' ? 'cmd.exe' : 'npm'
const npmArgs = process.platform === 'win32'
  ? ['/d', '/s', '/c', 'npm --version']
  : ['--version']
const npmVersion = execFileSync(npmCommand, npmArgs, { encoding: 'utf8' }).trim()

const [nodeMajor, nodeMinor] = nodeVersion.split('.').map(Number)
const npmMajor = Number(npmVersion.split('.')[0])
const supportedNode =
  (nodeMajor === 20 && nodeMinor >= 19) ||
  (nodeMajor === 22 && nodeMinor >= 12) ||
  nodeMajor >= 23

if (!supportedNode || npmMajor < 9) {
  console.error([
    'Unsupported frontend toolchain.',
    `Found Node.js ${nodeVersion} and npm ${npmVersion}.`,
    'GateLens requires Node.js ^20.19.0 or >=22.12.0, with npm >=9.',
    'With nvm: nvm install 24 && nvm use 24',
  ].join('\n'))
  process.exit(1)
}

console.log(`Frontend toolchain: Node.js ${nodeVersion}, npm ${npmVersion}`)
