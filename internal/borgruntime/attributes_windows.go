package borgruntime

import "syscall"

// markSystem pose l'attribut « système » qui fait d'un fichier commençant par
// la signature !<symlink> un lien aux yeux de Cygwin. Sans lui, le fichier
// reste un fichier ordinaire de quelques octets.
func markSystem(target string) error {
	name, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return syscall.SetFileAttributes(name, syscall.FILE_ATTRIBUTE_SYSTEM)
}
