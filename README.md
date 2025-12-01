# 🚀 GoPress

**GoPress** is a modern, ultra-lightweight dynamic blog system & CMS written in Go. It is designed to be as simple as Typecho but with the performance and concurrency of Go.

**GoPress** 是一个现代、极致轻量化的动态博客系统与 CMS，完全由 Go 语言编写。它的设计理念是对标 Typecho 的简洁易用，同时拥有 Go 语言的高性能与高并发特性。

---

## ✨ Features / 特性

- **📦 Single Binary / 单文件部署**: All assets (HTML/CSS/JS) are embedded. Just upload one file to run. (所有资源文件嵌入二进制，部署只需一个文件)
- **🔌 Dual-Engine Plugin System / 双引擎插件**: Write plugins in **JavaScript** (Goja) or **Go** (Yaegi). No compilation required, hot-reload supported. (支持 JS 和 Go 脚本编写插件，无需编译，热重载)
- **🎨 Typecho-like Theming / 经典主题系统**: Compatible with logic-less templates, supports live configuration and hot swapping. (支持在线配置、热切换，开发体验类似 Typecho)
- **⚡ HTMX & Tailwind / 现代前端**: SPA-like experience without page reloads, styled with Tailwind CSS. (无刷新页面切换体验)
- **🚀 High Performance / 高性能**: Powered by `Fiber` framework. (基于 Fiber 框架)
- **💾 Multi-DB Support / 多数据库**: SQLite (Default), MySQL, PostgreSQL.
- **🛠 Full Admin Panel / 完整后台**: Built-in article management, page creation, and system settings. (内置文章、页面、外观、插件管理面板)

## 🛠️ Quick Start / 快速开始

### Installation / 安装

1. Download the latest release from the [Releases](https://github.com/bcsm114514/GoPress/releases) page.
- 从 [Releases](https://github.com/bcsm114514/GoPress/releases) 页面下载最新版本。
2. Run the binary:
- 运行程序：
- For MacOS or Linux/对于MacOS或Linux
   ```bash
   ./gopress
   ```
- For Windows/对于Windows
   ```bash
   ./gopress.exe
   ```
3. Open http://localhost:3000 in your browser.
- 浏览器访问 http://localhost:3000。
4. Follow the installation wizard to set up your database and admin account.
- 跟随安装向导完成数据库和管理员设置。