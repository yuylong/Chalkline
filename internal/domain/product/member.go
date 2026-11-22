package product

import "fmt"

// MemberRole 是成员与产品的关系，决定产品级可见性与管理权（docs/domain/model.md）。
type MemberRole string

const (
	// MemberOwner 产品负责人：可管成员与产品设置。
	MemberOwner MemberRole = "owner"
	// MemberMember 正常协作。
	MemberMember MemberRole = "member"
	// MemberGuest 只读，适合外包与旁听；可读不可写、不可审批。
	MemberGuest MemberRole = "guest"
)

func (r MemberRole) Valid() bool {
	return r == MemberOwner || r == MemberMember || r == MemberGuest
}

// CanWrite 报告该产品角色是否具备写资格（细粒度写权限还叠加全局角色与负责人判定）。
func (r MemberRole) CanWrite() bool { return r == MemberOwner || r == MemberMember }

// Member 是成员与产品的多对多关系，产品级可见性的唯一依据。
// 每个成员在一个产品下只有一条记录（联合主键 product_id + user_id）。
type Member struct {
	ProductID  string
	UserID     string
	MemberRole MemberRole
	CreatedBy  string
}

// NewMember 构造并校验产品成员。
func NewMember(productID, userID string, role MemberRole, createdBy string) (Member, error) {
	m := Member{ProductID: productID, UserID: userID, MemberRole: role, CreatedBy: createdBy}
	if err := m.Validate(); err != nil {
		return Member{}, err
	}
	return m, nil
}

// Validate 检查字段级不变量。
func (m Member) Validate() error {
	if m.ProductID == "" || m.UserID == "" {
		return fmt.Errorf("%w: product_id 与 user_id 均不可为空", ErrValidation)
	}
	if !m.MemberRole.Valid() {
		return fmt.Errorf("%w: 产品角色非法 %q", ErrValidation, m.MemberRole)
	}
	return nil
}

// EnsureOwnerRetained 保证不变量 17：每个产品至少保留一个 owner。
// 从 members 中移除 removingUserID 前，剩余成员必须仍有 owner。
func EnsureOwnerRetained(members []Member, removingUserID string) error {
	if removingUserID == "" {
		return nil
	}
	for _, m := range members {
		if m.UserID != removingUserID && m.MemberRole == MemberOwner {
			return nil
		}
	}
	return fmt.Errorf("%w: 移除 %s 将使产品失去最后一个 owner", ErrValidation, removingUserID)
}
