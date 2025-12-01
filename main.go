package main

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopress/plugins"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/template/html/v2"
	"github.com/yuin/goldmark"
	"gorm.io/gorm"
)

// ==========================================
// 1. 嵌入静态资源
// ==========================================
//
//go:embed views themes plugins
var embeddedAssets embed.FS

// ==========================================
// 2. 自释放逻辑
// ==========================================
func restoreAssets() {
	log.Println("正在检查并释放静态资源...")
	err := fs.WalkDir(embeddedAssets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
			return nil
		}
		// 如果文件已存在，跳过（防止覆盖用户修改）
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		content, err := embeddedAssets.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, content, 0644); err != nil {
			return err
		}
		log.Println("释放资源:", path)
		return nil
	})
	if err != nil {
		log.Println("资源释放警告:", err)
	}
}

// 全局变量
var (
	shouldRestart      = false
	store              *session.Store
	GlobalSiteSettings map[string]string
)

func main() {
	// 启动时释放资源
	restoreAssets()

	for {
		shouldRestart = false
		runApp()
		if !shouldRestart {
			break
		}
		log.Println("正在重新加载系统...")
		time.Sleep(500 * time.Millisecond)
	}
}

// 加载站点设置
func LoadSiteSettings() {
	if DB == nil {
		return
	}
	var options []Option
	if err := DB.Find(&options).Error; err != nil {
		return
	}
	settings := make(map[string]string)
	settings["site_title"] = "GoPress"
	settings["site_description"] = "A simple blog."
	settings["site_url"] = "http://localhost:3000"
	settings["site_keywords"] = "blog, gopress"
	for _, opt := range options {
		settings[opt.Name] = opt.Value
	}
	GlobalSiteSettings = settings
}

// 扁平化主题配置
func FlattenThemeConfig() {
	CurrentThemeConfig.Config = make(map[string]string)
	for _, s := range CurrentThemeConfig.Settings {
		val := s.Value
		if val == "" {
			val = s.Default
		}
		CurrentThemeConfig.Config[s.Key] = val
	}
}

// 解压 ZIP
func Unzip(src string, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
	}
	return nil
}

func runApp() {
	isInstalled := LoadConfig()
	FlattenThemeConfig()

	// 初始化插件系统
	if isInstalled {
		plugins.Init()
	}

	engine := html.New(".", ".html")

	// 注册模板函数
	engine.AddFunc("safe", func(content string) template.HTML {
		return template.HTML(content)
	})

	engine.AddFunc("markdown", func(text string) template.HTML {
		text = plugins.ApplyFilter("OnMarkdown", text) // Hook
		var buf bytes.Buffer
		if err := goldmark.Convert([]byte(text), &buf); err != nil {
			return template.HTML("")
		}
		html := buf.String()
		html = plugins.ApplyFilter("OnContentRender", html) // Hook
		return template.HTML(html)
	})

	engine.AddFunc("summary", func(content string) template.HTML {
		parts := strings.Split(content, "<!--more-->")
		if len(parts) > 1 {
			var buf bytes.Buffer
			goldmark.Convert([]byte(parts[0]), &buf)
			return template.HTML(buf.String())
		}
		runes := []rune(content)
		if len(runes) > 200 {
			var buf bytes.Buffer
			goldmark.Convert([]byte(string(runes[:200])+"..."), &buf)
			return template.HTML(buf.String())
		}
		var buf bytes.Buffer
		goldmark.Convert([]byte(content), &buf)
		return template.HTML(buf.String())
	})

	if isInstalled {
		if err := ConnectDB(); err != nil {
			log.Println("DB连接失败:", err)
			isInstalled = false
		} else {
			LoadSiteSettings()
		}
	}

	store = session.New(session.Config{Expiration: 24 * time.Hour, CookieHTTPOnly: true})
	app := fiber.New(fiber.Config{
		Views:                 engine,
		DisableStartupMessage: true,
		BodyLimit:             20 * 1024 * 1024,
	})

	// === 安装模式 ===
	if !isInstalled {
		log.Println("运行在安装模式 :3000")
		app.Get("/", func(c *fiber.Ctx) error { return c.Redirect("/install") })
		app.Get("/install", func(c *fiber.Ctx) error { return c.Render("views/install", nil) })
		app.Post("/do-install", func(c *fiber.Ctx) error {
			dbType := c.FormValue("db_type")
			newConfig := Config{DBType: dbType, Theme: "default"}
			if dbType == "sqlite" {
				newConfig.DBName = c.FormValue("db_path")
			} else {
				newConfig.DBHost = c.FormValue("db_host")
				newConfig.DBPort = c.FormValue("db_port")
				newConfig.DBUser = c.FormValue("db_user")
				newConfig.DBPass = c.FormValue("db_pass")
				newConfig.DBName = c.FormValue("db_name")
			}
			GlobalConfig = newConfig
			if err := ConnectDB(); err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
			// 创建管理员
			hash, _ := HashPassword(c.FormValue("admin_pass"))
			DB.Create(&User{Username: c.FormValue("admin_user"), Password: hash, Nickname: c.FormValue("admin_nick")})
			// 写入设置
			DB.Save(&Option{Name: "site_title", Value: c.FormValue("site_title")})
			DB.Save(&Option{Name: "site_description", Value: c.FormValue("site_description")})
			// 保存配置
			if err := SaveConfig(newConfig); err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
			shouldRestart = true
			go func() { time.Sleep(1500 * time.Millisecond); app.Shutdown() }()
			return c.JSON(fiber.Map{"status": "ok"})
		})
	} else {
		// === 博客模式 ===
		log.Println("运行在博客模式 :3000")
		app.Static("/static", "./themes/"+GlobalConfig.Theme+"/static")

		themeDir := "themes/" + GlobalConfig.Theme
		themeLayout := themeDir + "/layout"
		adminLayout := "views/admin/layout"

		commonData := func(data fiber.Map) fiber.Map {
			data["Site"] = GlobalSiteSettings
			data["Theme"] = CurrentThemeConfig
			var navPages []Post
			if DB != nil {
				DB.Where("type = ? AND status = ?", "page", "published").Order("id asc").Find(&navPages)
			}
			data["NavPages"] = navPages
			return data
		}

		// --- 前台路由 ---
		app.Get("/", func(c *fiber.Ctx) error {
			var posts []Post
			DB.Where("type = ?", "post").Order("created_at desc").Find(&posts)
			return c.Render(themeDir+"/index", commonData(fiber.Map{
				"Title": GlobalSiteSettings["site_title"], "Posts": posts,
			}), themeLayout)
		})

		app.Get("/post/:slug", func(c *fiber.Ctx) error {
			var post Post
			if err := DB.Where("slug = ? AND type = ?", c.Params("slug"), "post").First(&post).Error; err != nil {
				return c.Status(404).SendString("Not Found")
			}
			return c.Render(themeDir+"/post", commonData(fiber.Map{
				"Title": post.Title + " - " + GlobalSiteSettings["site_title"], "Post": post,
			}), themeLayout)
		})

		// --- Sitemap ---
		app.Get("/sitemap.xml", func(c *fiber.Ctx) error {
			c.Set("Content-Type", "application/xml")

			// 1. 获取数据库中的文章/页面
			var items []Post
			DB.Where("status = ?", "published").Find(&items)

			baseURL := c.Protocol() + "://" + c.Hostname()

			// XML 头
			xml := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`

			// 首页
			xml += `
	<url>
		<loc>` + baseURL + `/</loc>
		<changefreq>daily</changefreq>
		<priority>1.0</priority>
	</url>`

			// 2. 遍历数据库内容
			for _, item := range items {
				path := "/"
				if item.Type == "post" {
					path = "/post/" + item.Slug
				} else {
					path = "/" + item.Slug
				}
				xml += `
	<url>
		<loc>` + baseURL + path + `</loc>
		<lastmod>` + item.UpdatedAt.Format("2006-01-02") + `</lastmod>
		<changefreq>weekly</changefreq>
		<priority>0.8</priority>
	</url>`
			}

			// === 遍历插件注册的路由 ===
			pluginRoutes := plugins.GetActiveRoutes()
			for _, path := range pluginRoutes {
				xml += `
	<url>
		<loc>` + baseURL + path + `</loc>
		<changefreq>weekly</changefreq>
		<priority>0.6</priority>
	</url>`
			}

			xml += `
</urlset>`
			return c.SendString(xml)
		})

		// --- 后台路由 ---
		admin := app.Group("/admin")
		admin.Get("/login", func(c *fiber.Ctx) error {
			return c.Render("views/admin/login", fiber.Map{"Error": c.Query("error")})
		})
		admin.Post("/login", func(c *fiber.Ctx) error {
			user := User{}
			if err := DB.Where("username = ?", c.FormValue("username")).First(&user).Error; err != nil {
				return c.Redirect("/admin/login?error=用户不存在")
			}
			if !CheckPasswordHash(c.FormValue("password"), user.Password) {
				return c.Redirect("/admin/login?error=密码错误")
			}
			sess, _ := store.Get(c)
			sess.Set("user_id", user.ID)
			sess.Save()
			return c.Redirect("/admin")
		})
		admin.Get("/logout", func(c *fiber.Ctx) error {
			sess, _ := store.Get(c)
			sess.Destroy()
			return c.Redirect("/admin/login")
		})

		// 鉴权拦截器
		admin.Use(func(c *fiber.Ctx) error {
			sess, _ := store.Get(c)
			if sess.Get("user_id") == nil {
				return c.Redirect("/admin/login")
			}
			return c.Next()
		})

		// 仪表盘
		admin.Get("/", func(c *fiber.Ctx) error {
			var count int64
			DB.Model(&Post{}).Where("type = ?", "post").Count(&count)
			return c.Render("views/admin/dashboard", fiber.Map{"Title": "仪表盘", "Active": "dashboard", "PostCount": count, "Theme": GlobalConfig}, adminLayout)
		})

		// 文章 & 页面管理
		admin.Get("/posts", func(c *fiber.Ctx) error {
			var posts []Post
			DB.Where("type = ?", "post").Order("created_at desc").Find(&posts)
			return c.Render("views/admin/list", fiber.Map{"Title": "文章列表", "Active": "posts", "Posts": posts, "Type": "post"}, adminLayout)
		})
		admin.Get("/pages", func(c *fiber.Ctx) error {
			var posts []Post
			DB.Where("type = ?", "page").Order("created_at desc").Find(&posts)
			return c.Render("views/admin/list", fiber.Map{"Title": "独立页面", "Active": "pages", "Posts": posts, "Type": "page"}, adminLayout)
		})
		admin.Get("/write", func(c *fiber.Ctx) error {
			pType := c.Query("type", "post")
			title, active := "撰写文章", "write"
			if pType == "page" {
				title, active = "创建页面", "pages"
			}
			return c.Render("views/admin/write", fiber.Map{"Title": title, "Active": active, "Post": Post{Type: pType}, "IsEdit": false}, adminLayout)
		})
		admin.Get("/posts/edit/:id", func(c *fiber.Ctx) error {
			var post Post
			if err := DB.First(&post, c.Params("id")).Error; err != nil {
				return c.Redirect("/admin/posts")
			}
			active := "posts"
			if post.Type == "page" {
				active = "pages"
			}
			return c.Render("views/admin/write", fiber.Map{"Title": "编辑内容", "Active": active, "Post": post, "IsEdit": true}, adminLayout)
		})
		admin.Post("/posts", func(c *fiber.Ctx) error {
			pType := c.FormValue("type")
			if pType == "" {
				pType = "post"
			}
			DB.Create(&Post{Title: c.FormValue("title"), Content: c.FormValue("content"), Slug: c.FormValue("slug"), Status: "published", Type: pType})
			if pType == "page" {
				return c.Redirect("/admin/pages")
			}
			return c.Redirect("/admin/posts")
		})
		admin.Post("/posts/update/:id", func(c *fiber.Ctx) error {
			var post Post
			if err := DB.First(&post, c.Params("id")).Error; err == nil {
				post.Title = c.FormValue("title")
				post.Slug = c.FormValue("slug")
				post.Content = c.FormValue("content")
				DB.Save(&post)
			}
			if post.Type == "page" {
				return c.Redirect("/admin/pages")
			}
			return c.Redirect("/admin/posts")
		})
		admin.Get("/posts/delete/:id", func(c *fiber.Ctx) error {
			var post Post
			DB.First(&post, c.Params("id"))
			DB.Delete(&post)
			if post.Type == "page" {
				return c.Redirect("/admin/pages")
			}
			return c.Redirect("/admin/posts")
		})

		// 外观管理
		admin.Get("/appearance", func(c *fiber.Ctx) error {
			entries, _ := os.ReadDir("./themes")
			type Info struct {
				ID, Name, Author, Version, Desc, Screenshot string
				IsActive                                    bool
			}
			var list []Info
			for _, e := range entries {
				if e.IsDir() {
					info := Info{ID: e.Name(), Name: e.Name(), IsActive: e.Name() == GlobalConfig.Theme}
					raw, _ := os.ReadFile("themes/" + e.Name() + "/config.json")
					var tmp ThemeConfig
					json.Unmarshal(raw, &tmp)
					if tmp.Name != "" {
						info.Name = tmp.Name
					}
					info.Author, info.Version, info.Desc, info.Screenshot = tmp.Author, tmp.Version, tmp.Description, tmp.Screenshot
					list = append(list, info)
				}
			}
			return c.Render("views/admin/appearance", fiber.Map{"Title": "网站外观", "Active": "appearance", "Themes": list, "CurrentTheme": GlobalConfig.Theme}, adminLayout)
		})
		admin.Get("/appearance/config/:id", func(c *fiber.Ctx) error {
			content, err := os.ReadFile("themes/" + c.Params("id") + "/config.json")
			if err != nil {
				return c.Status(404).JSON(fiber.Map{"error": "Config not found"})
			}
			return c.JSON(fiber.Map{"content": string(content)})
		})
		admin.Post("/appearance/save-config", func(c *fiber.Ctx) error {
			tid, _ := c.FormValue("theme_id"), c.FormValue("content")
			configPath := "themes/" + tid + "/config.json"
			raw, _ := os.ReadFile(configPath)
			var config ThemeConfig
			json.Unmarshal(raw, &config)
			for i, s := range config.Settings {
				val := c.FormValue(s.Key)
				config.Settings[i].Value = val
			}
			newJSON, _ := json.MarshalIndent(config, "", "  ")
			os.WriteFile(configPath, newJSON, 0644)
			if tid == GlobalConfig.Theme {
				CurrentThemeConfig = config
				FlattenThemeConfig()
			}
			return c.JSON(fiber.Map{"status": "ok", "message": "配置已保存"})
		})
		admin.Post("/appearance/upload", func(c *fiber.Ctx) error {
			file, err := c.FormFile("theme_zip")
			if err != nil {
				return c.Status(400).JSON(fiber.Map{"error": "Error"})
			}
			c.SaveFile(file, "./themes/"+file.Filename)
			Unzip("./themes/"+file.Filename, "./themes")
			os.Remove("./themes/" + file.Filename)
			return c.JSON(fiber.Map{"status": "ok"})
		})
		admin.Post("/appearance/delete", func(c *fiber.Ctx) error {
			tid := c.FormValue("theme_id")
			if tid == GlobalConfig.Theme || tid == "default" {
				return c.Status(400).JSON(fiber.Map{"error": "无法删除"})
			}
			os.RemoveAll("./themes/" + tid)
			return c.JSON(fiber.Map{"status": "ok"})
		})
		admin.Post("/appearance/activate", func(c *fiber.Ctx) error {
			tid := c.FormValue("theme_id")
			if tid == GlobalConfig.Theme {
				return c.JSON(fiber.Map{"status": "ok"})
			}
			GlobalConfig.Theme = tid
			SaveConfig(GlobalConfig)
			shouldRestart = true
			go func() { time.Sleep(500 * time.Millisecond); app.Shutdown() }()
			return c.JSON(fiber.Map{"status": "ok"})
		})

		// 系统设置 (GET)
		admin.Get("/settings", func(c *fiber.Ctx) error {
			return c.Render("views/admin/settings", fiber.Map{
				"Title":  "基本设置",
				"Active": "settings",
				"Site":   GlobalSiteSettings,
				// 传递 flash message (如果有)
				"Msg": c.Query("msg"),
				"Err": c.Query("err"),
			}, adminLayout)
		})

		// 系统设置 (POST)
		admin.Post("/settings", func(c *fiber.Ctx) error {
			// 1. 保存常规设置
			settings := map[string]string{
				"site_title":       c.FormValue("site_title"),
				"site_description": c.FormValue("site_description"),
				"site_url":         c.FormValue("site_url"),
				"site_keywords":    c.FormValue("site_keywords"),
			}
			for k, v := range settings {
				DB.Save(&Option{Name: k, Value: v})
			}

			// 2. 处理密码修改 (如果填了的话)
			newPass := c.FormValue("new_password")
			if newPass != "" {
				// 后端简单校验长度，防止绕过前端
				if len(newPass) < 8 {
					return c.Redirect("/admin/settings?err=密码太短")
				}

				// 获取当前用户ID
				sess, _ := store.Get(c)
				uid := sess.Get("user_id")

				hash, _ := HashPassword(newPass)
				if err := DB.Model(&User{}).Where("id = ?", uid).Update("password", hash).Error; err != nil {
					return c.Redirect("/admin/settings?err=密码修改失败")
				}
			}

			LoadSiteSettings()

			msg := "设置已保存"
			if newPass != "" {
				msg = "设置已保存，密码已修改"
			}
			return c.Redirect("/admin/settings?msg=" + msg)
		})

		// 插件管理
		admin.Get("/plugins", func(c *fiber.Ctx) error {
			list := plugins.GetAllPlugins()

			// 排序
			sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })

			return c.Render("views/admin/plugins", fiber.Map{
				"Title":   "插件管理",
				"Active":  "plugins",
				"Plugins": list,
			}, adminLayout)
		})
		admin.Post("/plugins/upload", func(c *fiber.Ctx) error {
			file, err := c.FormFile("plugin_zip")
			if err != nil {
				return c.Status(400).JSON(fiber.Map{"error": "Error"})
			}
			c.SaveFile(file, "./plugins/"+file.Filename)
			Unzip("./plugins/"+file.Filename, "./plugins")
			os.Remove("./plugins/" + file.Filename)
			plugins.Init()
			return c.JSON(fiber.Map{"status": "ok"})
		})
		admin.Post("/plugins/toggle", func(c *fiber.Ctx) error {
			id := c.FormValue("id")
			active := c.FormValue("active") == "true"

			instance, exists := plugins.GetInstance(id)
			if !exists {
				return c.Status(404).JSON(fiber.Map{"error": "Not found"})
			}

			jsonPath := "./plugins/" + instance.Meta.DirName + "/plugin.json"
			data, _ := os.ReadFile(jsonPath)

			var meta plugins.PluginMetadata
			json.Unmarshal(data, &meta)
			meta.Active = active

			newData, _ := json.MarshalIndent(meta, "", "  ")
			os.WriteFile(jsonPath, newData, 0644)

			plugins.Init()
			return c.JSON(fiber.Map{"status": "ok"})
		})

		// 删除插件
		admin.Post("/plugins/delete", func(c *fiber.Ctx) error {
			id := c.FormValue("id")

			// 使用 GetInstance()
			instance, exists := plugins.GetInstance(id)
			if !exists || instance.Meta.Active {
				return c.Status(400).JSON(fiber.Map{"error": "Cannot delete"})
			}

			os.RemoveAll("./plugins/" + instance.Meta.DirName)
			plugins.Init()
			return c.JSON(fiber.Map{"status": "ok"})
		})
		admin.Post("/plugins/reload", func(c *fiber.Ctx) error {
			plugins.Init()
			return c.JSON(fiber.Map{"status": "ok"})
		})

		// === 动态路由代理 (支持热重载和插件页面) ===
		app.All("/*", func(c *fiber.Ctx) error {
			// 1. 尝试匹配插件路由
			result, found := plugins.MatchRoute(c.Method(), c.Path())

			if found {
				// 情况 A: 返回 Map (使用主题渲染)
				if dataMap, ok := result.(map[string]interface{}); ok {
					if tmplName, ok := dataMap["template"].(string); ok {
						mockPost := Post{
							Title: "", Content: "", Slug: c.Path(), Type: "page",
							Model: gorm.Model{CreatedAt: time.Now()},
						}
						if t, ok := dataMap["title"].(string); ok {
							mockPost.Title = t
						}
						if cnt, ok := dataMap["content"].(string); ok {
							mockPost.Content = cnt
						}
						return c.Render(themeDir+"/"+tmplName, commonData(fiber.Map{
							"Title":      mockPost.Title + " - " + GlobalSiteSettings["site_title"],
							"Post":       mockPost,
							"PluginData": dataMap,
						}), themeLayout)
					}
				}

				// === 修复点：情况 B: 返回字符串 (Raw HTML) ===
				if str, ok := result.(string); ok {
					// 强制设置 Content-Type 为 HTML，否则浏览器会把它当纯文本显示
					c.Set("Content-Type", "text/html; charset=utf-8")
					return c.SendString(str)
				}

				// 情况 C: 其他类型 (JSON)
				return c.JSON(result)
			}

			// 2. 尝试匹配独立页面
			// 排除系统路径
			if strings.HasPrefix(c.Path(), "/admin") || strings.HasPrefix(c.Path(), "/static") {
				return c.Next()
			}
			var post Post
			// Slug 匹配 (去掉开头的 /)
			slug := strings.TrimPrefix(c.Path(), "/")
			if err := DB.Where("slug = ? AND type = ?", slug, "page").First(&post).Error; err == nil {
				return c.Render(themeDir+"/page", commonData(fiber.Map{
					"Title": post.Title + " - " + GlobalSiteSettings["site_title"],
					"Post":  post,
				}), themeLayout)
			}

			// 3. 404
			return c.Status(404).SendString("404 Not Found")
		})
	}

	if err := app.Listen(":3000"); err != nil {
		log.Println(err)
	}
}
