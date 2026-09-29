/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useState } from 'react';
import {
  InputNumber,
  Modal,
  RadioGroup,
  Radio,
  Typography,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../../helpers';
import { useTranslation } from 'react-i18next';

/** 与 dto.VolcAssetLimitUnlimited 对齐。 */
const UNLIMITED = -1;

/** 从用户的 setting JSON 里取出提额覆盖值；没有或坏了都按「未覆盖」处理。 */
function readLimit(user) {
  if (!user?.setting) return 0;
  try {
    const parsed = JSON.parse(user.setting);
    return Number(parsed.volc_asset_limit ?? 0);
  } catch {
    return 0;
  }
}

function modeOf(limit) {
  if (limit === UNLIMITED) return 'unlimited';
  if (limit > 0) return 'custom';
  return 'default';
}

/**
 * 素材库提额。
 *
 * 三档而不是一个数字输入框：光给输入框的话，「回落到全局默认」和「不限」
 * 都得靠管理员记住 0 和 -1 两个哨兵值，没人记得住。
 */
const UserAssetLimitModal = ({ visible, onCancel, onSuccess, user }) => {
  const { t } = useTranslation();
  const [mode, setMode] = useState('default');
  const [amount, setAmount] = useState(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!visible) return;
    const current = readLimit(user);
    setMode(modeOf(current));
    setAmount(current > 0 ? current : null);
  }, [visible, user]);

  const handleOk = async () => {
    let value = 0;
    if (mode === 'unlimited') {
      value = UNLIMITED;
    } else if (mode === 'custom') {
      if (!amount || amount <= 0) {
        showError(t('请填写大于 0 的条数'));
        return;
      }
      value = Math.floor(amount);
    }

    setLoading(true);
    try {
      const res = await API.post('/api/user/manage', {
        id: user.id,
        action: 'set_volc_asset_limit',
        value,
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess(t('素材库额度已更新'));
      onSuccess && onSuccess();
      onCancel();
    } catch (error) {
      showError(error);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title={t('素材库额度')}
      visible={visible}
      onCancel={onCancel}
      onOk={handleOk}
      confirmLoading={loading}
    >
      <Typography.Text type='tertiary' style={{ display: 'block' }}>
        {user?.username
          ? t('目标用户：{{username}}', { username: user.username })
          : ''}
      </Typography.Text>
      <RadioGroup
        value={mode}
        onChange={(e) => setMode(e.target.value)}
        direction='vertical'
        style={{ marginTop: 12 }}
      >
        <Radio value='default'>{t('使用站点默认值')}</Radio>
        <Radio value='custom'>{t('自定义条数')}</Radio>
        <Radio value='unlimited'>{t('不限')}</Radio>
      </RadioGroup>
      {mode === 'custom' && (
        <InputNumber
          value={amount}
          onChange={setAmount}
          min={1}
          precision={0}
          placeholder={t('该用户可保留的素材条数')}
          style={{ marginTop: 12, width: '100%' }}
        />
      )}
    </Modal>
  );
};

export default UserAssetLimitModal;
