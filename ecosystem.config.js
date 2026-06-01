// PM2 process config for preorder-bot (Go binary).
//
// Build static (no CGO — sqlite is pure Go) then start:
//   CGO_ENABLED=0 go build -o preorder-bot .
//   cp config.example.json config.json   # lalu isi secret-nya
//   pm2 start ecosystem.config.js
//   pm2 save
//
// interpreter 'none' makes PM2 exec the binary directly instead of via node.
// cwd must stay at the project dir so ./static and ./config.json resolve.
module.exports = {
  apps: [
    {
      name: 'preorder-bot',
      script: './preorder-bot',
      args: '-config config.json',
      interpreter: 'none',
      cwd: __dirname,
      autorestart: true,
      max_restarts: 10,
      restart_delay: 5000,
      max_memory_restart: '256M',
      out_file: './logs/out.log',
      error_file: './logs/error.log',
      merge_logs: true,
      time: true,
    },
  ],
};
