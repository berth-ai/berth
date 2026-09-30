" Berth: load Berth's Neovim defaults unless the user has an init file.
if !filereadable(stdpath('config') . '/init.lua') && !filereadable(stdpath('config') . '/init.vim')
  source /etc/berth/defaults/nvim/init.lua
endif
