// Command server exposes ADK session routes with AIO sandbox lifecycle.
// VEADK_AIO_IDLE_TIMEOUT and VEADK_AIO_PAUSED_RETENTION configure cleanup.
package main

import (
	"context"
	"log"
	"os"
	"strings"

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	var spaces []string
	if value := strings.TrimSpace(os.Getenv("SKILL_SPACE_ID")); value != "" {
		spaces = strings.Split(value, ",")
	}
	root, err := veagent.New(&veagent.Config{SkillsMode: "aio_sandbox", Skills: spaces})
	if err != nil {
		log.Fatal(err)
	}
	// Persist ADK history and orchestration metadata together. For replicas on
	// different hosts, use a shared PostgreSQL/MySQL database instead of SQLite.
	if err := os.MkdirAll(".adk", 0700); err != nil {
		log.Fatal(err)
	}
	dialector := sqlite.Open(".adk/aio.db?_busy_timeout=5000")
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	defer sqlDB.Close()
	backend, err := database.NewSessionService(dialector)
	if err != nil {
		log.Fatal(err)
	}
	if err := database.AutoMigrate(backend); err != nil {
		log.Fatal(err)
	}
	store, err := agentkit.NewOrderedSessionService(backend, db)
	if err != nil {
		log.Fatal(err)
	}
	coordination, err := agentkit.NewGormAIOLifecycleStore(db)
	if err != nil {
		log.Fatal(err)
	}
	lifecycle, err := agentkit.NewAIOLifecycle(agentkit.AIOLifecycleConfig{
		SessionService: store,
		Store:          coordination,
		SkillSpaceIDs:  spaces,
		// Optional: IdleTimeout and PausedRetention override the environment.
	})
	if err != nil {
		log.Fatal(err)
	}
	app := agentkit_server_app.NewAgentkitServerApp(
		apps.DefaultApiConfig().SetSimpleAPIEnabled(false), agentkit_server_app.WithAIOLifecycle(lifecycle),
	)
	if err := app.Run(context.Background(), &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(root), SessionService: store,
	}); err != nil {
		log.Fatal(err)
	}
}
