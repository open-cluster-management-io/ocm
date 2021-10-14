package utils

import (
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestConfigChecker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Checker Suite")
}

var cc *configChecker
var fileAName, fileBName string

var _ = Describe("Config Checker With Reload", func() {
	BeforeEach(func() {
		// create file in tmp directory
		fileA, err := ioutil.TempFile("", "fileA")
		Expect(err).To(BeNil())

		_, err = fileA.WriteString("fileA")
		Expect(err).To(BeNil())

		fileB, err := ioutil.TempFile("", "fileB")
		Expect(err).To(BeNil())

		_, err = fileB.WriteString("fileB")
		Expect(err).To(BeNil())

		fileAName, fileBName = fileA.Name(), fileB.Name()

		// update checksum in checker in every cases
		cc, err = NewConfigChecker("test", fileAName, fileBName)
		Expect(err).To(BeNil())
		cc.SetReload(true)
	})

	Context("No files Changed", func() {
		It("should return be no err", func() {
			err := cc.Check(nil)
			Expect(err).To(BeNil())
		})
	})

	Context("Delete file A", func() {
		It("should return err at  both first check and second check", func() {
			// delete file A
			Expect(os.Remove(fileAName)).To(BeNil())
			// check
			Expect(cc.Check(nil)).ToNot(BeNil())
			// check again err still not be nil
			Expect(cc.Check(nil)).ToNot(BeNil())
		})
	})

	Context("Modify file A", func() {
		It("should return err at first check, nil at second check", func() {
			// modify file A
			Expect(ioutil.WriteFile(fileAName, []byte("fileA modified"), 0755)).To(BeNil())
			// check
			Expect(cc.Check(nil)).ToNot(BeNil())
			// check again err should be nil
			Expect(cc.Check(nil)).To(BeNil())
		})
	})

	Context("Modify file B", func() {
		It("should return err at first check, nil at second check", func() {
			// modify file B
			Expect(ioutil.WriteFile(fileBName, []byte("fileB modified"), 0755)).To(BeNil())
			// check
			Expect(cc.Check(nil)).ToNot(BeNil())
			// check again err should be nil
			Expect(cc.Check(nil)).To(BeNil())
		})
	})
})
