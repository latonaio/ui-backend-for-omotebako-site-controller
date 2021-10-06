package handlers

import (
	"fmt"
	"net/http"
	"ui-backend-for-omotebako-site-controller/app/database"
	"ui-backend-for-omotebako-site-controller/app/models"

	"context"

	"github.com/gin-gonic/gin"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

func GetLatestTimestamp(c *gin.Context, db *database.Database) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	row, err := models.CSVUploadTransactions(
		qm.Select("*"),
		qm.OrderBy("timestamp DESC"),
	).One(ctx, db.DB)
	if err != nil {
		sugar.Errorf("failed to get csv_upload_transaction: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"timestamp": nil})
		return
	}
	sugar.Debugf("csv_upload_transaction record: %p", row)
	if row == nil {
		sugar.Info("no csv information")
		c.JSON(http.StatusOK, gin.H{"timestamp": nil})
		return
	}

	timestampStr := row.Timestamp.String
	timestampVal := fmt.Sprintf(`%s/%s/%s %s:%s:%s`, timestampStr[0:4], timestampStr[4:6], timestampStr[6:8], timestampStr[8:10], timestampStr[10:12], timestampStr[12:14])
	sugar.Infof("latest timestamp: %s", timestampVal)
	c.JSON(http.StatusOK, gin.H{"timestamp": timestampVal})
	return
}
