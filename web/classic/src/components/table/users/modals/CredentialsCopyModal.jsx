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

import React, { useEffect, useMemo, useState } from 'react';
import { Modal, Button, Space, Typography } from '@douyinfe/semi-ui';
import { IconCopy } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../../../helpers';

const { Text } = Typography;

const KEY_PREFIX = 'auto_create_user_setting.';

const DEFAULT_COPY_TEMPLATES = [
  { label: '站点', template: '{{site}}' },
  { label: '用户名', template: '{{username}}' },
  { label: '密码', template: '{{password}}' },
];

function renderCopyItem(template, values) {
  if (!template) return '';
  return template
    .split('{{username}}')
    .join(values.username)
    .split('{{password}}')
    .join(values.password)
    .split('{{site}}')
    .join(values.site);
}

async function copyToClipboard(text) {
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* fall through */
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}

/**
 * Renders the post-create "copy these credentials" popup using the configured
 * auto_create_user_setting.copy_templates rows with {{username}}, {{password}}
 * and {{site}} substituted in.
 */
const CredentialsCopyModal = ({ visible, credentials, onClose }) => {
  const { t } = useTranslation();
  const [settings, setSettings] = useState({
    site_url: '',
    copy_templates: DEFAULT_COPY_TEMPLATES,
  });

  // Fetch settings once when the popup is opened. We re-fetch every time so
  // that an admin changing the templates sees the new ones on their next create.
  useEffect(() => {
    if (!visible) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await API.get('/api/option/');
        const list = res?.data?.data || [];
        const next = {
          site_url: '',
          copy_templates: DEFAULT_COPY_TEMPLATES,
        };
        for (const item of list) {
          if (!item?.key?.startsWith?.(KEY_PREFIX)) continue;
          const sub = item.key.slice(KEY_PREFIX.length);
          if (sub === 'site_url') {
            next.site_url = item.value || '';
          } else if (sub === 'copy_templates') {
            try {
              const parsed = JSON.parse(item.value);
              if (Array.isArray(parsed)) {
                next.copy_templates = parsed.filter(
                  (it) =>
                    it &&
                    typeof it.label === 'string' &&
                    typeof it.template === 'string',
                );
              }
            } catch {
              /* keep defaults */
            }
          }
        }
        if (!cancelled) setSettings(next);
      } catch {
        /* keep defaults; admin can still copy what we have */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [visible]);

  const rows = useMemo(() => {
    if (!credentials) return [];
    return settings.copy_templates.map((item) => ({
      label: item.label,
      text: renderCopyItem(item.template, {
        username: credentials.username,
        password: credentials.password,
        site: settings.site_url,
      }),
    }));
  }, [credentials, settings]);

  const handleCopyRow = async (row) => {
    const ok = await copyToClipboard(row.text);
    if (ok) showSuccess(t('已复制'));
    else showError(t('复制失败'));
  };

  const handleCopyAll = async () => {
    const blob = rows.map((r) => `${r.label}: ${r.text}`).join('\n');
    const ok = await copyToClipboard(blob);
    if (ok) showSuccess(t('已复制'));
    else showError(t('复制失败'));
  };

  return (
    <Modal
      title={t('用户已创建')}
      visible={visible}
      onCancel={onClose}
      footer={
        <Space>
          {rows.length > 0 && (
            <Button theme='solid' onClick={handleCopyAll}>
              {t('全部复制')}
            </Button>
          )}
          <Button onClick={onClose}>{t('关闭')}</Button>
        </Space>
      }
    >
      <Text type='tertiary' size='small'>
        {t(
          '请通过安全渠道复制并分享这些凭据，关闭后将不再显示。',
        )}
      </Text>
      {rows.length === 0 ? (
        <div className='mt-3 text-sm text-gray-500'>
          {t(
            '暂未配置复制项，请前往 系统设置 → 运营 → 自动创建用户 添加。',
          )}
        </div>
      ) : (
        <div className='mt-3 flex flex-col gap-2'>
          {rows.map((row, idx) => (
            <div
              key={idx}
              className='flex items-center gap-2 rounded-md border p-2'
            >
              <div className='min-w-[64px] text-xs text-gray-500'>
                {row.label}
              </div>
              <div className='flex-1 break-all text-sm font-mono'>
                {row.text || (
                  <span className='italic text-gray-400'>{t('（空）')}</span>
                )}
              </div>
              <Button
                icon={<IconCopy />}
                size='small'
                theme='borderless'
                onClick={() => handleCopyRow(row)}
              >
                {t('复制')}
              </Button>
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
};

export default CredentialsCopyModal;
