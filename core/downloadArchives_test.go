package core

import (
	"testing"
	"database/sql"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/glacier"
	"bytes"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"strings"
	"rsg/utils"
	"os"
	"errors"
	"rsg/awsutils"
)

func mockStartPartialRetrieveJob(glacierMock *GlacierMock, vault, archiveId, bytesRange, jobIdToReturn string) *mock.Call {
	var retrievalByteRange *string = nil
	if (bytesRange != "") {
		retrievalByteRange = aws.String(bytesRange)
	}
	params := &glacier.InitiateJobInput{
		AccountId: aws.String(awsutils.AccountId),
		VaultName: aws.String(vault),
		JobParameters: &glacier.JobParameters{
			ArchiveId: aws.String(archiveId),
			Type:        aws.String("archive-retrieval"),
			RetrievalByteRange: retrievalByteRange,
		},
	}

	out := &glacier.InitiateJobOutput{
		JobId: aws.String(jobIdToReturn),
	}
	return glacierMock.On("InitiateJob", params).Return(out, nil)
}

func mockStartPartialRetrieveJobWithError(glacierMock *GlacierMock, vault, archiveId, bytesRange string, errorToReturn error) *mock.Call {
	var retrievalByteRange *string = nil
	if (bytesRange != "") {
		retrievalByteRange = aws.String(bytesRange)
	}
	params := &glacier.InitiateJobInput{
		AccountId: aws.String(awsutils.AccountId),
		VaultName: aws.String(vault),
		JobParameters: &glacier.JobParameters{
			ArchiveId: aws.String(archiveId),
			Type:        aws.String("archive-retrieval"),
			RetrievalByteRange: retrievalByteRange,
		},
	}

	return glacierMock.On("InitiateJob", params).Return(nil, errorToReturn)
}

func mockStartPartialRetrieveJobForAny(glacierMock *GlacierMock, jobIdToReturn string) *mock.Call {
	out := &glacier.InitiateJobOutput{
		JobId: aws.String(jobIdToReturn),
	}
	return glacierMock.On("InitiateJob", mock.AnythingOfType("*glacier.InitiateJobInput")).Return(out, nil)
}

func mockPartialOutputJob(glacierMock *GlacierMock, jobId, vault, bytesRange string, content []byte) *mock.Call {
	params := &glacier.GetJobOutputInput{
		AccountId: aws.String(awsutils.AccountId),
		JobId:     aws.String(jobId),
		VaultName: aws.String(vault),
		Range: aws.String(bytesRange),
	}

	out := &glacier.GetJobOutputOutput{
		Body:  newReaderClosable(bytes.NewReader(content)),
	}

	return glacierMock.On("GetJobOutput", params).Return(out, nil)
}

func mockPartialOutputJobForAny(glacierMock *GlacierMock, content []byte) *mock.Call {
	out := &glacier.GetJobOutputOutput{
		Body:  newReaderClosable(bytes.NewReader(content)),
	}

	return glacierMock.On("GetJobOutput", mock.AnythingOfType("*glacier.GetJobOutputInput")).Return(out, nil)
}

func mockDescribeJobForAny(glacierMock *GlacierMock, completed bool) *mock.Call {
	out := &glacier.JobDescription{
		Completed: aws.Bool(completed),
	}

	return glacierMock.On("DescribeJob", mock.AnythingOfType("*glacier.DescribeJobInput")).Return(out, nil)
}

func TestDownloadArchives_retrieve_and_download_file_in_one_part(t *testing.T) {
	// Given
	CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 1,
		archivesRetrievalMaxSize: utils.S_1MB,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 1,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file1.txt', 'archiveId1', 5);")
	db.Close()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "0-4", "jobId1")
	mockDescribeJob(glacierMock, "jobId1", restorationContext.Vault, true)
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "0-4", []byte("hello"))

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/file1.txt", "hello")
}

func TestDownloadArchives_retrieve_and_download_file_with_multipart(t *testing.T) {
	// Given
	CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file1.txt', 'archiveId1', 4194304);")
	db.Close()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "0-2097151", "jobId1").Once()
	mockDescribeJob(glacierMock, "jobId1", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "0-1048799", []byte(strings.Repeat("_", 1048800))).Once()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "2097152-3145727", "jobId2").Once()
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "1048800-2097151", []byte(strings.Repeat("_", 1048352))).Once()
	mockDescribeJob(glacierMock, "jobId2", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId2", restorationContext.Vault, "0-447", []byte(strings.Repeat("_", 448))).Once()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "3145728-4194303", "jobId3").Once()
	mockPartialOutputJob(glacierMock, "jobId2", restorationContext.Vault, "448-1048575", []byte(strings.Repeat("_", 1048128))).Once()
	mockDescribeJob(glacierMock, "jobId3", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId3", restorationContext.Vault, "0-671", []byte(strings.Repeat("_", 672)))

	mockPartialOutputJob(glacierMock, "jobId3", restorationContext.Vault, "672-1048575", append([]byte(strings.Repeat("_", 1047899)), []byte("hello")...))

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/file1.txt", strings.Repeat("_", 4194299) + "hello")
}

func TestDownloadArchives_retrieve_and_download_2_files_with_multipart(t *testing.T) {
	// Given
	CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file1.txt', 'archiveId1', 4194304);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file2.txt', 'archiveId2', 2097152);")
	db.Close()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "0-2097151", "jobId1").Once()
	mockDescribeJob(glacierMock, "jobId1", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "0-1048799", []byte(strings.Repeat("_", 1048800))).Once()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "2097152-3145727", "jobId2").Once()
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "1048800-2097151", []byte(strings.Repeat("_", 1048352))).Once()
	mockDescribeJob(glacierMock, "jobId2", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId2", restorationContext.Vault, "0-447", []byte(strings.Repeat("_", 448))).Once()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "3145728-4194303", "jobId3").Once()
	mockPartialOutputJob(glacierMock, "jobId2", restorationContext.Vault, "448-1048575", []byte(strings.Repeat("_", 1048128))).Once()
	mockDescribeJob(glacierMock, "jobId3", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId3", restorationContext.Vault, "0-671", []byte(strings.Repeat("_", 672)))

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId2", "0-1048575", "jobId4").Once()
	mockPartialOutputJob(glacierMock, "jobId3", restorationContext.Vault, "672-1048575", append([]byte(strings.Repeat("_", 1047899)), []byte("hello")...))
	mockDescribeJob(glacierMock, "jobId4", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId4", restorationContext.Vault, "0-895", []byte(strings.Repeat("_", 896)))

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId2", "1048576-2097151", "jobId5").Once()
	mockPartialOutputJob(glacierMock, "jobId4", restorationContext.Vault, "896-1048575", []byte(strings.Repeat("_", 1047680)))
	mockDescribeJob(glacierMock, "jobId5", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId5", restorationContext.Vault, "0-1119", []byte(strings.Repeat("_", 1120)))

	mockPartialOutputJob(glacierMock, "jobId5", restorationContext.Vault, "1120-1048575", append([]byte(strings.Repeat("_", 1047451)), []byte("olleh")...))

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/file1.txt", strings.Repeat("_", 4194299) + "hello")
	assertFileContent(t, "../../testtmp/dest/share/data/file2.txt", strings.Repeat("_", 2097147) + "olleh")
}

func TestDownloadArchives_retrieve_and_download_3_files_with_2_identical(t *testing.T) {
	// Given
	CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file1.txt', 'archiveId1', 4194304);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file2.txt', 'archiveId2', 2097152);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file3.txt', 'archiveId1', 4194304);")
	db.Close()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "0-2097151", "jobId1").Once()
	mockDescribeJob(glacierMock, "jobId1", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "0-1048799", []byte(strings.Repeat("_", 1048800))).Once()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "2097152-3145727", "jobId2").Once()
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "1048800-2097151", []byte(strings.Repeat("_", 1048352))).Once()
	mockDescribeJob(glacierMock, "jobId2", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId2", restorationContext.Vault, "0-447", []byte(strings.Repeat("_", 448))).Once()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "3145728-4194303", "jobId3").Once()
	mockPartialOutputJob(glacierMock, "jobId2", restorationContext.Vault, "448-1048575", []byte(strings.Repeat("_", 1048128))).Once()
	mockDescribeJob(glacierMock, "jobId3", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId3", restorationContext.Vault, "0-671", []byte(strings.Repeat("_", 672)))

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId2", "0-1048575", "jobId4").Once()
	mockPartialOutputJob(glacierMock, "jobId3", restorationContext.Vault, "672-1048575", append([]byte(strings.Repeat("_", 1047899)), []byte("hello")...))
	mockDescribeJob(glacierMock, "jobId4", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId4", restorationContext.Vault, "0-895", []byte(strings.Repeat("_", 896)))

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId2", "1048576-2097151", "jobId5").Once()
	mockPartialOutputJob(glacierMock, "jobId4", restorationContext.Vault, "896-1048575", []byte(strings.Repeat("_", 1047680)))
	mockDescribeJob(glacierMock, "jobId5", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId5", restorationContext.Vault, "0-1119", []byte(strings.Repeat("_", 1120)))

	mockPartialOutputJob(glacierMock, "jobId5", restorationContext.Vault, "1120-1048575", append([]byte(strings.Repeat("_", 1047451)), []byte("olleh")...))

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/file1.txt", strings.Repeat("_", 4194299) + "hello")
	assertFileContent(t, "../../testtmp/dest/share/data/file2.txt", strings.Repeat("_", 2097147) + "olleh")
	assertFileContent(t, "../../testtmp/dest/share/data/file3.txt", strings.Repeat("_", 4194299) + "hello")
}

func TestDownloadArchives_retrieve_and_download_only_filtered_files(t *testing.T) {
	// Given
	CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	restorationContext.Options.Filters = []string{"data/folder/*", "*.info", "data/file??.bin", "data/iwantthis" }
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file1.txt', 'archiveId1', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file2.bin', 'archiveId2', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folderno/no.bin', 'archiveId3', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/no', 'archiveId4', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/otherfolder/no', 'archiveId5', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/otherfolder/file3.info', 'archiveId6', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/otherfolder/no.txt', 'archiveId7', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file4.info', 'archiveId8', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file41.bin', 'archiveId9', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file42.bin', 'archiveId10', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/filenop.bin', 'archiveId11', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/iwantthis', 'archiveId12', 2);")
	db.Close()

	mockStartPartialRetrieveJobForAny(glacierMock, "jobId")
	mockDescribeJobForAny(glacierMock, true)
	mockPartialOutputJobForAny(glacierMock, []byte("ok"))

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file1.txt", "ok")
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file2.bin", "ok")
	assertFileDoestntExist(t, "../../testtmp/dest/share/data/folder/no.bin")
	assertFileDoestntExist(t, "../../testtmp/dest/share/data/no")
	assertFileDoestntExist(t, "../../testtmp/dest/share/data/otherfolder/no")
	assertFileContent(t, "../../testtmp/dest/share/data/otherfolder/file3.info", "ok")
	assertFileDoestntExist(t, "../../testtmp/dest/share/data/otherfolder/no.txt")
	assertFileContent(t, "../../testtmp/dest/share/data/file4.info", "ok")
	assertFileContent(t, "../../testtmp/dest/share/data/file41.bin", "ok")
	assertFileContent(t, "../../testtmp/dest/share/data/file42.bin", "ok")
	assertFileDoestntExist(t, "../../testtmp/dest/share/data/filenop.bin")
	assertFileContent(t, "../../testtmp/dest/share/data/iwantthis", "ok")
}

func TestDownloadArchives_compute_total_size(t *testing.T) {
	// Given
	buffer := CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	restorationContext.Options.Filters = []string{"data/folder/*", "*.info", "data/file??.bin", "data/iwantthis" }
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file1.txt', 'archiveId1', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file2.bin', 'archiveId2', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file3.txt', 'archiveId1', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/no', 'archiveId3', 2);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/nop', 'archiveId1', 2);")
	db.Close()

	mockStartPartialRetrieveJobForAny(glacierMock, "jobId")
	mockDescribeJobForAny(glacierMock, true)
	mockPartialOutputJobForAny(glacierMock, []byte("ok"))

	// When
	downloadContext.downloadArchives()

	// Then
	assert.Contains(t, string(buffer.Bytes()), "4B to restore")

}

func TestDownloadArchives_retrieve_and_download_an_archive_already_started(t *testing.T) {
	// Given
	CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file1.txt', 'archiveId1', 1048581);")
	db.Close()

	ioutil.WriteFile("../../testtmp/dest/archiveId1", append([]byte(strings.Repeat("_", 1048576)), []byte("hel")...), 0700)

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "1048576-1048580", "jobId1").Once()
	mockDescribeJob(glacierMock, "jobId1", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "0-4", []byte("hello")).Once()

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file1.txt", strings.Repeat("_", 1048576) + "hello")
}

func TestDownloadArchives_retrieve_and_download_an_archive_already_started_and_completed(t *testing.T) {
	// Given
	CommonInitTest()
	_, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file1.txt', 'archiveId1', 1048581);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file2.txt', 'archiveId1', 1048581);")
	db.Close()

	ioutil.WriteFile("../../testtmp/dest/archiveId1", append([]byte(strings.Repeat("_", 1048576)), []byte("hello")...), 0700)

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file1.txt", strings.Repeat("_", 1048576) + "hello")
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file2.txt", strings.Repeat("_", 1048576) + "hello")
}

func TestDownloadArchives_retrieve_when_archive_is_not_found(t *testing.T) {
	// Given
	CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file1.txt', 'archiveId1', 1);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file2.txt', 'archiveId2', 1);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file3.txt', 'archiveId3', 1);")
	db.Close()

	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId1", "0-0", "jobId1").Once()
	mockDescribeJob(glacierMock, "jobId1", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "0-0", []byte("1")).Once()

	mockStartPartialRetrieveJobWithError(glacierMock, restorationContext.Vault, "archiveId2", "0-0", errors.New("ResourceNotFoundException")).Once()
	mockStartPartialRetrieveJob(glacierMock, restorationContext.Vault, "archiveId3", "0-0", "jobId2").Once()
	mockDescribeJob(glacierMock, "jobId2", restorationContext.Vault, true).Once()
	mockPartialOutputJob(glacierMock, "jobId2", restorationContext.Vault, "0-0", []byte("3")).Once()


	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file1.txt", "1")
	assertFileDoestntExist(t, "../../testtmp/dest/share/data/folder/file2.txt")
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file3.txt", "3")
}

func TestDownloadArchives_retrieve_empty_file(t *testing.T) {
	// Given
	CommonInitTest()
	_, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 3496, // 1048800 on 5 min
		archivesRetrievalMaxSize: utils.S_1MB * 2,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 10,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file1.txt', 'GlacierZeroSizeFile', 0);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/folder/file2.txt', 'GlacierZeroSizeFile', 0);")
	db.Close()

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file1.txt", "")
	assertFileContent(t, "../../testtmp/dest/share/data/folder/file2.txt", "")
}

func TestDownloadArchives_retrieve_and_download_when_retrieval_job_exists(t *testing.T) {
	// Given
	CommonInitTest()
	glacierMock, restorationContext := InitTestWithGlacier()
	downloadContext := DownloadContext{
		restorationContext: restorationContext,
		speedInBytesBySec: 1,
		archivesRetrievalMaxSize: utils.S_1MB,
		speedAutoUpdate: false,
		archivesRetrievalSize: 0,
		archivePartRetrievalListMaxSize: 1,
		archivePartRetrieveList: nil,
		hasArchiveRows: false,
		db: nil,
		archiveRows:nil,
	}

	db, _ := sql.Open("sqlite3", restorationContext.GetMappingFilePath())
	db.Exec("CREATE TABLE `file_info_tb` (`key` INTEGER PRIMARY KEY AUTOINCREMENT, `basePath` TEXT,`archiveID` TEXT, fileSize INTEGER);")
	db.Exec("INSERT INTO `file_info_tb` (shareName, basePath, archiveID, fileSize) VALUES ('share', 'data/file1.txt', 'archiveId1', 5);")
	db.Close()

	awsutils.AddRetrievalJobAtStartup("archiveId1", "0-4", "jobId1")
	mockDescribeJob(glacierMock, "jobId1", restorationContext.Vault, true)
	mockPartialOutputJob(glacierMock, "jobId1", restorationContext.Vault, "0-4", []byte("hello"))

	// When
	downloadContext.downloadArchives()

	// Then
	assertFileContent(t, "../../testtmp/dest/share/data/file1.txt", "hello")
}

func assertFileContent(t *testing.T, filePath, expected string) {
	data, _ := ioutil.ReadFile(filePath)
	assert.Equal(t, expected, string(data))
}

func assertFileDoestntExist(t *testing.T, filePath string) {
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		assert.Fail(t, "path should not exist")
	}
}