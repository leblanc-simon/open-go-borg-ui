package fsperm

import "golang.org/x/sys/windows"

// ownerAccess est ce que le propriétaire garde : lire, écrire, supprimer et
// gérer les droits. Pas d'exécution : Cygwin lira le fichier comme 0600.
const ownerAccess = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE |
	windows.DELETE | windows.READ_CONTROL | windows.WRITE_DAC | windows.WRITE_OWNER

// restrict remplace la DACL par une entrée unique accordée à l'utilisateur
// courant, et la protège de l'héritage : les entrées de SYSTEM et des
// administrateurs, héritées du profil, disparaissent.
func restrict(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: ownerAccess,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil)
}
