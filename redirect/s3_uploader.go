package redirect

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"io"
	"log"
	"os"
)

type S3Uploader struct {
	Endpoint   string `value:"s3.endpoint"`
	Region     string `value:"s3.region"`
	SecretId   string `value:"s3.secretId"`
	SecretKey  string `value:"s3.secretKey"`
	BucketName string `value:"s3.bucketName"`
	client     *s3.S3
}

func (s *S3Uploader) AfterPropertiesSet() {
	config := &aws.Config{
		Region:           aws.String(s.Region),
		Endpoint:         &s.Endpoint,
		S3ForcePathStyle: aws.Bool(true),
		Credentials:      credentials.NewStaticCredentials(s.SecretId, s.SecretKey, ""),
	}
	sess, err := session.NewSession(config)
	if err != nil {
		log.Fatal("初始化S3客户端失败", err)
	}
	s.client = s3.New(sess)
	log.Println("聊天文件上传已启用")
}

func (s *S3Uploader) FUpload(objectName string, src string) (string, error) {
	fp, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer fp.Close()
	return s.Upload(objectName, fp)
}

func (s *S3Uploader) Upload(objectName string, src io.ReadSeeker) (string, error) {
	out, err := s.client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(objectName),
		Body:   src,
	})
	if err != nil {
		return "", err
	}
	return *out.ETag, nil
}
