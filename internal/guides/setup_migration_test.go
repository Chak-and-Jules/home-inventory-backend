package guides

import (
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSetupMigrationPreservesMergedLifecycleState(t *testing.T) {
	f := newFixture(t)
	p := models.GuideProgress{UserID: f.user, GuideID: "introduction", Revision: 1, Version: 9, WebStatus: InProgress, WebStepID: "definition", WebHomeID: &f.home, MobileStatus: Completed}
	require.NoError(t, f.s.DB.Create(&p).Error)
	// Recreate the schema shape shipped by #146, then apply the additive migration.
	for _, column := range []string{"web_definition_id", "web_inventory_id", "mobile_definition_id", "mobile_inventory_id"} {
		require.NoError(t, f.s.DB.Migrator().DropColumn(&models.GuideProgress{}, column))
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, f.s.DB.AutoMigrate(&models.GuideProgress{}, &models.GuideRequest{}))
	}
	list, err := f.s.List(ctx, f.user, Web, 2, &f.home)
	require.NoError(t, err)
	require.EqualValues(t, 9, list[0].Version)
	require.Equal(t, InProgress, list[0].State.Status)
	require.Equal(t, "definition", list[0].State.StepID)
	require.Equal(t, &f.home, list[0].State.HomeID)
	require.Nil(t, list[0].State.DefinitionID)
	require.Nil(t, list[0].State.InventoryID)
	mobile, err := f.s.List(ctx, f.user, Mobile, 2, &f.home)
	require.NoError(t, err)
	require.Equal(t, Completed, mobile[0].State.Status)
	require.False(t, mobile[0].ShouldOffer)
	// Existing renderer-v1 commands remain valid after migration.
	c := command(Web, 9, "start")
	c.HomeID = &f.home
	v, err := f.s.Act(ctx, f.user, "introduction", c)
	require.NoError(t, err)
	require.EqualValues(t, 10, v.Version)
	require.Equal(t, "definition", v.State.StepID)
}
