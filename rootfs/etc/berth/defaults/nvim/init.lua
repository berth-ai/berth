-- Berth's default Neovim configuration. Create ~/.config/nvim/init.lua to replace it.
local o = vim.opt
o.number = true
o.relativenumber = true
o.mouse = "a"
o.clipboard = "unnamedplus"
o.ignorecase = true
o.smartcase = true
o.expandtab = true
o.shiftwidth = 2
o.tabstop = 2
o.undofile = true
o.signcolumn = "yes"
o.termguicolors = true
o.splitright = true
o.splitbelow = true
vim.g.mapleader = " "
vim.keymap.set("n", "<leader>w", "<cmd>write<cr>", { desc = "Write" })
vim.keymap.set("n", "<Esc>", "<cmd>nohlsearch<cr>")
