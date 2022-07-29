package tools

import "os"

func PathExist(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}

func CreatePathIfNotExists(path string, mode os.FileMode) error {
	exist, err := PathExist(path)
	if err != nil {
		return err
	}
	if !exist {
		err = os.MkdirAll(path, mode)
		if err != nil {
			return err
		}
	}
	return nil
}
