package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	scCsv "ui-backend-for-omotebako-site-controller/app/csv"
	"ui-backend-for-omotebako-site-controller/app/file"
	"ui-backend-for-omotebako-site-controller/app/helper"
	"ui-backend-for-omotebako-site-controller/app/models"
	"ui-backend-for-omotebako-site-controller/pkg"

	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"golang.org/x/xerrors"
)

type reservationGuest struct {
	ReservationID xxx
	GuestID       xxx
	*scCsv.ReservationData
}

type ErrorStruct struct {
	CustomerName        string
	CustomerPhoneNumber string
	ErrorMsg            string
}

var sugar = pkg.NewSugaredLogger()

func (d *Database) TransactionReservationInfo(reservations []*scCsv.ReservationData, ctx context.Context) (map[int]ErrorStruct, error) {
	var reservationGuests []*reservationGuest
	errorMap := map[int]ErrorStruct{}

	for i, reservation := range reservations {
		tx, err := d.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, xerrors.Errorf("failed to begin transaction: %w", err)
		}
		switch reservation.Notice {
		case "予約":
			reservationGuest, err := d.addReservationInfoToDB(reservation, tx, ctx)
			if err != nil {
				errorMap[i] = ErrorStruct{
					CustomerName:        reservation.ReservationHolder,
					CustomerPhoneNumber: reservation.ReservationHolderPhoneNumber,
					ErrorMsg:            fmt.Sprint(err),
				}
				if err := tx.Rollback(); err != nil {
					return nil, xerrors.Errorf("Rolleback is uncompleted: %w", err)
				}
				continue
			}
			if err := tx.Commit(); err != nil {
				return nil, xerrors.Errorf("Database Commit is uncompleted: %w", err)
			}
			reservationGuests = append(reservationGuests, reservationGuest)
		case "取消":
			err := deleteReservationInfoFromDB(reservation, reservationGuests, tx, ctx)
			if err != nil {
				errorMap[i] = ErrorStruct{
					CustomerName:        reservation.ReservationHolder,
					CustomerPhoneNumber: reservation.ReservationHolderPhoneNumber,
					ErrorMsg:            fmt.Sprint(err),
				}
				if err := tx.Rollback(); err != nil {
					return nil, xerrors.Errorf("Rolleback is uncompleted: %w", err)
				}
				continue
			}
			if err := tx.Commit(); err != nil {
				return nil, xerrors.Errorf("Database Commit is uncompleted: %w", err)
			}
		default:
			errorMap[i] = ErrorStruct{
				CustomerName:        reservation.ReservationHolder,
				CustomerPhoneNumber: reservation.ReservationHolderPhoneNumber,
				ErrorMsg:            fmt.Sprintf("unknown reservation type :%v", reservation.Notice),
			}
		}
	}

	// エラーが１件でも存在したらその情報を返す
	if len(errorMap) != 0 {
		return errorMap, nil
	}

	return nil, nil
}

func (d *Database) GetCsvExecutionErrorsWithCsvUploadTransactionByStatus(ctx context.Context, status int) (models.CSVExecutionErrorSlice, error) {
	rows, err := models.CSVExecutionErrors(
		qm.Select("*"),
		models.CSVExecutionErrorWhere.Status.EQ(status),
		qm.Load(models.CSVExecutionErrorRels.CSV),
	).All(ctx, d.DB)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (d *Database) GetCsvUploadTransactionByTimeStamp(ctx context.Context, timestamp string) (models.CSVUploadTransactionSlice, error) {
	queries_csv_transaction := []qm.QueryMod{
		qm.Where(models.CSVUploadTransactionColumns.Timestamp+"=?", timestamp),
	}
	csvTransaction, err := models.CSVUploadTransactions(queries_csv_transaction...).All(ctx, d.DB)
	if err != nil {
		return nil, err
	}
	return csvTransaction, nil
}

func (d *Database) GetCsvExecutionErrorsByStatus(ctx context.Context, status int) (models.CSVExecutionErrorSlice, error) {
	rows, err := models.CSVExecutionErrors(
		models.CSVExecutionErrorWhere.Status.EQ(status),
	).All(ctx, d.DB)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (d *Database) addReservationInfoToDB(reservation *scCsv.ReservationData, tx *sql.Tx, ctx context.Context) (*reservationGuest, error) {
	currentTime := time.Now()
	newReservationGuest := reservationGuest{
		//ReservationID:   0,
		//GuestID:         0,
		ReservationData: reservation,
	}

	stayDateFrom, err := checkInTime(reservation)
	if err != nil {
		sugar.Errorf("failed to parse reservationDate: %v\n", err)
		// エラーメッセージ：チェックイン日エラー
		return &newReservationGuest, fmt.Errorf("チェックイン日が不正か入力されていません。")
	}

	stayDateTo, err := time.Parse("20060102", reservation.StayDateTo)
	if err != nil {
		sugar.Errorf("failed to parse stayDateTo: %v\n", err)
		// エラーメッセージ：チェックアウト日エラー
		return &newReservationGuest, fmt.Errorf("チェックアウト日が不正か入力されていません。")
	}

	reservationDate, err := time.Parse("20060102", reservation.ReservatioinDate)
	if err != nil {
		sugar.Errorf("failed to parse reservationDate: %v\n", err)
		// エラーメッセージ：予約受信日エラー
		return &newReservationGuest, fmt.Errorf("予約受信日が不正か入力されていません。")
	}

	reservationMethodId, err := checkReservationMethod(reservation, ctx, tx)
	if err != nil {
		sugar.Errorf("invalid reservation method: %v", err)
		// エラーメッセージ：予約経路エラー
		return &newReservationGuest, fmt.Errorf("予約経路が不正か入力されていません。")
	}

	paymentMethodId, err := checkPaymentMethod(reservation.PaymentMethodName, ctx, tx)
	if err != nil {
		sugar.Errorf("failed to insert payment method error: %v", err)
	}

	planId := checkProductMaster(reservation.ProductCode, reservation.ProductName, ctx, tx)

	if Results := validateReservationData(reservation); Results != nil {
		sugar.Errorf("validation reservation data error: %v", Results)
		//	エラーメッセージ：validation エラー
		ResultsStr := fmt.Sprint(strings.Join(Results, ", "))
		return &newReservationGuest, fmt.Errorf("必要な項目が入力されていません。: %v", ResultsStr)
	}

	guest := checkNewGuest(reservation, ctx, tx)

	// Insert reservationの準備
	newReservation := models.Reservation{
		// ReservationID:         int
		//GuestID:               null.IntFrom(newGuests.GuestID),
		ReservationHolder:     null.StringFrom(reservation.ReservationHolder),
		ReservationHolderKana: null.StringFrom(reservation.ReservationHolderKana),
		StayDateFrom:          null.TimeFrom(stayDateFrom),
		StayDateTo:            null.TimeFrom(stayDateTo),
		StayDays:              null.Int16From(reservation.StayDays),
		NumberOfRooms:         null.Int16From(reservation.NumberOfRooms),
		NumberOfGuests:        null.Int16From(reservation.NumberOfGuestsMale + reservation.NumberOfGuestsFemale),
		NumberOfGuestsMale:    null.Int16From(reservation.NumberOfGuestsMale),
		NumberOfGuestsFemale:  null.Int16From(reservation.NumberOfGuestsFemale),
		HasChild:              null.Int8From(checkChild(reservation)),
		ProductID:             null.StringFromPtr(planId),
		ReservationMethod:     null.IntFrom(reservationMethodId), 
		PaymentMethod:         null.IntFrom(paymentMethodId),
		Coupon:                null.IntFrom(0), //【要検討】0:未, 1:有, 2:無
		// StatusCode:            null.Int8From(0),   // default:0が指定される
		Plan:       null.StringFrom(reservation.ProductName),
		UpdateDate: null.TimeFrom(currentTime),
		// IsCheckin:             null.Int8From(1),   // defaultで0:未が指定される
		ReservationDate: null.TimeFrom(reservationDate),
		PaymentStatus:   null.IntFrom(0), // defaultとして0:未を指定
		// NewGuestFlag:          null.Int8From(1),   // defaultで0が指定される
		// DeleteFlag:            null.IntFrom(1),   // defaultで0が指定される
	}

	if guest == nil {
		//	新規顧客
		sugar.Info("新規顧客")
		// Insert guest
		newGuests := models.Guest{
			// GuestID:       int
			Name:     null.StringFrom(reservation.Name),
			NameKana: null.StringFrom(reservation.NameKana),
			Gender:   null.IntFrom(1), // defaultとして1:女性を指定
			// GenderByFace:  null.StringFrom(),
			// AgeByFace:     null.Float32From(),
			// BirthDate:     null.TimeFrom(),
			// Age:           null.IntFrom(30),   // defaultとして30を指定？
			GuestEmail:  null.StringFrom(reservation.Email),
			PhoneNumber: null.StringFrom(reservation.PhoneNumber),
			PostalCode:  null.StringFrom(helper.PostalCodeFormat(reservation.PostalCode)),
			HomeAddress: null.StringFrom(reservation.HomeAddress),
			CreateDate:  null.TimeFrom(currentTime),
			UpdateDate:  null.TimeFrom(currentTime),
			// FaceIDAzure:   null.StringFrom(),
			// FaceImagePath: null.StringFrom(),
			// DeleteFlag:    null.Int8From(0),  // default: 0
		}
		if err := newGuests.Insert(ctx, tx, boil.Infer()); err != nil {
			sugar.Errorf("failed to insert Guset record: %v", err)
			// エラーメッセージ：顧客登録エラー
			return &newReservationGuest, fmt.Errorf("顧客情報の登録に失敗しました。")
		}

		//
		newReservation.GuestID = null.IntFrom(newGuests.GuestID)
	} else {
		//	既存顧客
		sugar.Infof("既存顧客 guest_id: %d", guest.GuestID)
		guest.GuestEmail = null.StringFrom(reservation.Email)
		guest.PhoneNumber = null.StringFrom(reservation.PhoneNumber)
		guest.PostalCode = null.StringFrom(helper.PostalCodeFormat(reservation.PostalCode))
		guest.HomeAddress = null.StringFrom(reservation.HomeAddress)

		if _, err := guest.Update(ctx, tx, boil.Whitelist(
			models.GuestColumns.GuestEmail,
			models.GuestColumns.PhoneNumber,
			models.GuestColumns.PostalCode,
			models.GuestColumns.HomeAddress,
		)); err != nil {
			sugar.Errorf("failed to update Guset record: %v", err)
			// エラーメッセージ：顧客更新エラー
			return &newReservationGuest, fmt.Errorf("顧客情報の更新に失敗しました。")
		}

		newReservation.GuestID = null.IntFrom(guest.GuestID)
		newReservation.NewGuestFlag = null.Int8From(1)
	}

	if err := newReservation.Insert(ctx, tx, boil.Infer()); err != nil {
		sugar.Errorf("failed to insert Reservation record: %v", err)
		// エラーメッセージ：予約登録エラー
		return &newReservationGuest, fmt.Errorf("予約情報の登録に失敗しました。")
	}
	sugar.Infof("added reservation ID: %v, Name: %v\n", newReservation.GuestID, newReservation.ReservationHolder)
	newReservationGuest = reservationGuest{
		ReservationID:   newReservation.ReservationID,
		ReservationData: reservation,
	}
	sugar.Debugf("reservation guest: %v", newReservationGuest)

	return &newReservationGuest, nil
}

func deleteReservationInfoFromDB(reservation *scCsv.ReservationData, reservationGuests []*reservationGuest, tx *sql.Tx, ctx context.Context) error {
	// Set updating columns
	updCols := map[string]interface{}{
		models.ReservationColumns.DeleteFlag: 1,
	}

	targetID, err := selectDeleteReservationID(reservation, reservationGuests, tx, ctx)
	if err != nil || targetID == 0 {
		return err
	}

	query := qm.Where(models.ReservationColumns.ReservationID+"=?", targetID)

	// update!
	_, err = models.Reservations(query).UpdateAll(ctx, tx, updCols)
	if err != nil {
		sugar.Errorf("failed to update reservation delete flag: %v", err)
		// エラーメッセージ：reservationのdelete_flag更新エラー
		return fmt.Errorf("予約のキャンセルに失敗しました。")
	}

	return nil
}

func selectDeleteReservationID(reservation *scCsv.ReservationData, reservationGuests []*reservationGuest, tx *sql.Tx, ctx context.Context) (int, error) {
	if Results := validateDeleteReservationData(reservation); Results != nil {
		sugar.Errorf("validation delete reservation data error: %v", Results)
		//	エラーメッセージ：validation エラー
		ResultsStr := fmt.Sprint(strings.Join(Results, ", "))
		return 0, fmt.Errorf("必要な項目が入力されていません。: %v", ResultsStr)
	}

	// 団体者名、電話番号、住所 → guestIDの特定
	queries_guest := []qm.QueryMod{
		//qm.Select("guest_id"),
		qm.Where(models.GuestColumns.Name+"=?", reservation.Name),
		qm.And(models.GuestColumns.NameKana+"=?", reservation.NameKana),
		qm.And(models.GuestColumns.PhoneNumber+"=?", reservation.PhoneNumber),
		// qm.And(models.GuestColumns.HomeAddress+"=?", reservation.PostalCode + reservation.HomeAddress),
	}
	guests, err := models.Guests(queries_guest...).All(ctx, tx)
	if err != nil {
		sugar.Errorf("failed to get guest records: %v", err)
		return 0, fmt.Errorf("キャンセルする顧客の取得に失敗しました。")
	}

	// WhereIn method needs to pass a slice of interface{}
	var guestIDs []interface{}
	for _, v := range guests {
		sugar.Debugf("guestID: %d", v.GuestID)
		guestIDs = append(guestIDs, v.GuestID)
	}

	// guest_id, stayDateFrom, stayDateTo　→　reservationIdの特定
	queries_reservation := []qm.QueryMod{
		qm.WhereIn(models.ReservationColumns.GuestID+" IN ?", guestIDs),
		// qm.And(models.ReservationColumns.ReservationHolder+"=?", reservation.ReservationHolder),
		// qm.And(models.ReservationColumns.ReservationHolderKana+"=?", reservation.ReservationHolderKana),
		// qm.And(models.ReservationColumns.StayDateFrom+"=?", reservation.StayDateFrom+reservation.CheckInTime),  //
		qm.And(models.ReservationColumns.StayDateTo+"=?", reservation.StayDateTo),
		qm.And(models.ReservationColumns.StayDays+"=?", reservation.StayDays),
		qm.And(models.ReservationColumns.NumberOfRooms+"=?", reservation.NumberOfRooms),
		qm.And(models.ReservationColumns.NumberOfGuests+"=?", reservation.NumberOfGuests),
	}

	counts, err := models.Reservations(queries_reservation...).Count(ctx, tx)
	if counts == 0 {
		sugar.Debug("No reservation")
		for _, reservationGuest := range reservationGuests {
			sugar.Debugf("reservationGuest: %v, %v, %v", reservationGuest.Name, reservationGuest.NameKana, reservationGuest.PhoneNumber)
			if reservation.Name == reservationGuest.Name && reservation.NameKana == reservationGuest.NameKana && reservation.PhoneNumber == reservationGuest.PhoneNumber {
				return reservationGuest.ReservationID, nil
			}
		}
		sugar.Errorf("no reservation, name: %s, phone number: %s", reservation.Name, reservation.PhoneNumber)
		// エラーメッセージ：キャンセル予約が登録されていない
		return 0, fmt.Errorf("キャンセルする予約が登録されていません。")
	} else if counts > 1 {
		sugar.Errorf("multiple reservations exist, name: %v, phone number: %v", reservation.Name, reservation.PhoneNumber)
		// エラーメッセージ：キャンセル予約が複数登録されている
		return 0, fmt.Errorf("キャンセルする同一予約が複数登録されています。")
	} else if err != nil {
		sugar.Errorf("cannot detect delete reservationID: %v\n", err)
		// エラーメッセージ：キャンセル予約を特定できなかった
		return 0, fmt.Errorf("キャンセルする予約を特定できませんでした。")
	}

	reservationId, err := models.Reservations(queries_reservation...).All(ctx, tx)
	if err != nil {
		sugar.Errorf("failed to get reservation ID: %v", err)
		// エラーメッセージ：reservationのgetエラー
		return 0, fmt.Errorf("キャンセルする予約の取得に失敗しました。")
	}
	sugar.Infof("delete ReservationID: %v, Name: %v\n", reservationId[0].ReservationID, reservationId[0].ReservationHolder)

	return reservationId[0].ReservationID, nil
}

// TODO: websocket実装時に必要かも
func (d *Database) SelectErrorCSVRowsWithIds(csvIds []int, ctx context.Context, tx *sql.Tx) (models.CSVExecutionErrorSlice, error) {
	// whereinに合わせて型を変換する
	var ids []interface{}
	for _, v := range csvIds {
		ids = append(ids, v)
	}
	// query := []qm.QueryMod{
	// 	qm.Select("*"),
	// 	qm.From("csv_execution_errors"),
	// 	qm.InnerJoin("csv_upload_transaction on csv_upload_transaction.id = csv_execution_errors.csv_id"),
	// 	// qm.WhereIn(models.CSVExecutionErrorColumns.ID+" IN ?", csvIds),
	// 	// qm.WhereIn(fmt.Sprintf("%s in ?", models.CSVExecutionErrorColumns.ID), ids...),
	// }
	rows, err := models.CSVExecutionErrors(qm.InnerJoin("csv_upload_transaction on csv_upload_transaction.id = csv_execution_errors.csv_id")).All(ctx, tx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("%v", *rows[0])
	// rows, err := models.CSVExecutionErrors(query...).All(ctx, tx)
	// if err != nil {
	// 	return nil, err
	// }
	return rows, nil
}

func (d *Database) RegisterCSVDataToDB(ctx context.Context, file file.File, path string, id int, siteControllerName string) error {
	// サイトコントローラー名
	sugar.Infof("site controller name is %s", siteControllerName)

	// トランザクション：insertReservation, insertGuest
	csvPath := fmt.Sprintf("%s/%s", path, file.Name)
	var reservations []*scCsv.ReservationData
	var err error
	switch siteControllerName {
	case "xxxx":
		reservations, err = scCsv.ImportFromLincoln(csvPath)
	case "xxxx":
		// TODO xxxx用のインポート関数を作る
		reservations, err = scCsv.ImportFromLincoln(csvPath)
	case "xxxx":
		// TODO xxxx用のインポート関数を作る
		reservations, err = scCsv.ImportFromLincoln(csvPath)
	default:
		return xerrors.Errorf("site controller name '%s' is not available", siteControllerName)
	}
	if err != nil {
		if err := d.updateCsvUploadTransactionStatusToError(id, ctx); err != nil {
			return xerrors.Errorf("failed to upload csv_upload_transaction status: %w", err)
		}
		return xerrors.Errorf("path: %s, failed to import csv: %w", csvPath, err)
	}

	errors, err := d.TransactionReservationInfo(reservations, ctx)

	// トランザクションOK...csvステータスをcompleteに変える
	if err == nil && errors == nil {
		if err := d.finishCsvUpload(id, ctx); err != nil {
			return fmt.Errorf("failed to upload csv_upload_transaction status: %v", err)
		} else {
			sugar.Info("successful of csv uploading")
		}
	} else {
		// トランザクションERROR...csvステータスをerrorに変える
		if err := d.updateCsvUploadTransactionStatusToError(id, ctx); err != nil {
			return fmt.Errorf("failed to upload csv_upload_transaction status: %v", err)
		} else {
			sugar.Info("failed to upload csv")
		}
		// csv_execution_errorにerror内容を入れる
		// TODO 戻り値のidsをチャネル使ってwebsocketに渡す
		ids := d.InsertCSVExecutionError(ctx, errors, id)
		sugar.Debugf("error ids: %v", ids)
	}
	return nil
}

func (d *Database) CreateCsvUploadTransaction(ctx context.Context, fileName string, createdTime time.Time, timestamp string, path string) (*models.CSVUploadTransaction, error) {
	// mysqlにinsertするデータを作成
	newCSVUploadTransaction := models.CSVUploadTransaction{
		FileName:             null.StringFrom(fileName),
		Status:               null.StringFrom("before"),
		CreatedTimeInWindows: null.TimeFrom(createdTime),
		Timestamp:            null.StringFrom(timestamp),
		Path:                 null.StringFrom(path),
	}

	// mysqlにinsertする
	if err := newCSVUploadTransaction.Insert(ctx, d.DB, boil.Infer()); err != nil {
		return nil, err
	}

	return &newCSVUploadTransaction, nil
}

func (d *Database) GetCSVUpdateTransaction(ctx context.Context) (models.CSVUploadTransactionSlice, error) {
	q := models.CSVUploadTransactions(
		qm.Select("id", "file_name", "created_time_in_windows"),
		qm.OrderBy("created_time_in_windows DESC"),
	)

	rows, err := q.All(ctx, d.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to get records of csv_upload_transaction records: %v", err)
	}

	return rows, nil
}

func (d *Database) updateCsvUploadTransactionStatusToError(id int, ctx context.Context) error {
	record := models.CSVUploadTransaction{
		ID:     id,
		Status: null.StringFrom("ERROR"),
	}

	err := record.Upsert(ctx, d.DB, boil.Whitelist(models.CSVUploadTransactionColumns.Status), boil.Infer())
	if err != nil {
		return err
	}

	return nil
}

func (d *Database) finishCsvUpload(id int, ctx context.Context) error {
	record, err := models.CSVUploadTransactions(
		qm.Where(models.CSVUploadTransactionColumns.ID+" = ?", id),
	).One(ctx, d.DB)
	if err != nil {
		return xerrors.Errorf("failed to get csv transaction data: %w", err)
	}
	if record.Status.String == "complete" {
		return xerrors.New("csv upload status is already complete")
	}

	record.Status = null.StringFrom("complete")
	_, err = record.Update(ctx, d.DB, boil.Infer())
	if err != nil {
		return xerrors.Errorf("failed to get csv transaction data: %w", err)
	}
	return nil
}

func (d *Database) InsertCSVExecutionError(ctx context.Context, mapError map[int]ErrorStruct, csvId int) []int {
	var ids []int
	// mysqlにinsertするデータを生成
	for i, errStruct := range mapError {
		newCSVExecutionError := models.CSVExecutionError{
			//ID:                  null.IntFrom(),
			LineNumber:          i + 1,
			CustomerName:        null.StringFrom(errStruct.CustomerName),
			CustomerPhoneNumber: null.StringFrom(errStruct.CustomerPhoneNumber),
			ErrorMessage:        errStruct.ErrorMsg,
			Status:              0, //未対応は0
			CSVID:               csvId,
		}

		if err := newCSVExecutionError.Insert(ctx, d.DB, boil.Infer()); err != nil {
			sugar.Errorf("failed to insert new record to csv_execution_errors: line number: %d, error message: %v", i+1, err)
		}

		// ID = 0はDB insertエラーを表します
		ids = append(ids, newCSVExecutionError.ID)
	}
	return ids
}
