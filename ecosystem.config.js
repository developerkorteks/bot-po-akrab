// PM2 process config for multi-provider preorder services.
//
// Build binaries first:
//   mkdir -p bin
//   CGO_ENABLED=0 go build -o bin/preorder-khfy .
//   CGO_ENABLED=0 go build -o bin/preorder-ics ./cmd/ics-backend
//   CGO_ENABLED=0 go build -o bin/preorder-aggregator ./cmd/aggregator
//
// Ensure configs exist in project root:
//   config.json
//   config.ics.json
//   config.aggregator.json
//
// Then start:
//   mkdir -p logs
//   pm2 start ecosystem.config.js
//   pm2 save
//
// interpreter 'none' makes PM2 exec the binaries directly instead of via node.
// cwd must stay at the project dir so configs, logs, and static assets resolve correctly.
module.exports = {
  apps: [
    {
      name: 'preorder-khfy',
      script: './bin/preorder-khfy',
      args: '-config config.json',
      interpreter: 'none',
      cwd: __dirname,
      autorestart: true,
      max_restarts: 10,
      restart_delay: 5000,
      max_memory_restart: '256M',
      out_file: './logs/khfy-out.log',
      error_file: './logs/khfy-error.log',
      merge_logs: true,
      time: true,
    },
    {
      name: 'preorder-ics',
      script: './bin/preorder-ics',
      args: '-config config.ics.json',
      interpreter: 'none',
      cwd: __dirname,
      autorestart: true,
      max_restarts: 10,
      restart_delay: 5000,
      max_memory_restart: '256M',
      out_file: './logs/ics-out.log',
      error_file: './logs/ics-error.log',
      merge_logs: true,
      time: true,
    },
    {
      name: 'preorder-aggregator',
      script: './bin/preorder-aggregator',
      args: '-config config.aggregator.json',
      interpreter: 'none',
      cwd: __dirname,
      autorestart: true,
      max_restarts: 10,
      restart_delay: 5000,
      max_memory_restart: '192M',
      out_file: './logs/aggregator-out.log',
      error_file: './logs/aggregator-error.log',
      merge_logs: true,
      time: true,
    },
  ],
};
