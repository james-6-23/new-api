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

import React, { useState, useRef, useEffect, useCallback } from 'react';
import { API, showError, showSuccess } from '../../../../helpers';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import {
  Button,
  SideSheet,
  Space,
  Spin,
  Typography,
  Card,
  Tag,
  Form,
  Row,
  Col,
} from '@douyinfe/semi-ui';
import { IconSave, IconClose, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';

const { Text, Title } = Typography;

/**
 * Auto Create User modal.
 *
 * On open, fetches a server-generated suggestion from GET /api/user/auto/preview
 * and pre-fills the form. The admin may edit any field before confirming;
 * on submit we POST /api/user/ with the (possibly edited) values, then
 * hand off to CredentialsCopyModal via the `onCreated` callback.
 */
const AutoCreateUserModal = ({ visible, handleClose, refresh, onCreated }) => {
  const { t } = useTranslation();
  const formApiRef = useRef(null);
  const [loading, setLoading] = useState(false);
  const [previewLoading, setPreviewLoading] = useState(false);
  const isMobile = useIsMobile();

  const fetchPreview = useCallback(async () => {
    setPreviewLoading(true);
    try {
      const res = await API.get('/api/user/auto/preview');
      const { success, message, data } = res.data || {};
      if (!success || !data) {
        showError(message || t('获取建议失败'));
        return;
      }
      formApiRef.current?.setValues({
        username: data.username,
        password: data.password,
        group: data.group,
        quota: data.quota,
      });
    } catch (e) {
      showError(t('获取建议失败'));
    } finally {
      setPreviewLoading(false);
    }
  }, [t]);

  // Pull a fresh suggestion each time the drawer opens.
  useEffect(() => {
    if (visible) {
      fetchPreview();
    }
  }, [visible, fetchPreview]);

  const submit = async (values) => {
    setLoading(true);
    try {
      const payload = {
        username: (values.username || '').trim(),
        password: values.password || '',
        display_name: (values.username || '').trim(),
        role: 1, // Common user — matches the existing AddUserModal default.
        group: values.group,
        quota: Number(values.quota) || 0,
      };
      const res = await API.post('/api/user/', payload);
      const { success, message } = res.data || {};
      if (!success) {
        showError(message || t('创建用户失败'));
        return;
      }
      showSuccess(t('用户已创建'));
      refresh?.();
      onCreated?.({
        username: payload.username,
        password: payload.password,
        group: payload.group,
        quota: payload.quota,
      });
      handleClose();
    } catch (e) {
      showError(t('创建用户失败'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <SideSheet
      placement='left'
      title={
        <Space>
          <Tag color='blue' shape='circle'>
            {t('自动')}
          </Tag>
          <Title heading={4} className='m-0'>
            {t('自动创建用户')}
          </Title>
        </Space>
      }
      bodyStyle={{ padding: 0 }}
      visible={visible}
      width={isMobile ? '100%' : 600}
      footer={
        <div className='flex justify-end bg-white'>
          <Space>
            <Button
              theme='light'
              icon={<IconRefresh />}
              onClick={fetchPreview}
              loading={previewLoading}
            >
              {t('重新生成')}
            </Button>
            <Button
              theme='solid'
              onClick={() => formApiRef.current?.submitForm()}
              icon={<IconSave />}
              loading={loading}
            >
              {t('确认创建')}
            </Button>
            <Button
              theme='light'
              type='primary'
              onClick={handleClose}
              icon={<IconClose />}
            >
              {t('取消')}
            </Button>
          </Space>
        </div>
      }
      closeIcon={null}
      onCancel={handleClose}
    >
      <Spin spinning={loading || previewLoading}>
        <Form
          initValues={{ username: '', password: '', group: 'default', quota: 0 }}
          getFormApi={(api) => (formApiRef.current = api)}
          onSubmit={submit}
          onSubmitFail={(errs) => {
            const first = Object.values(errs)[0];
            if (first) showError(Array.isArray(first) ? first[0] : first);
            formApiRef.current?.scrollToError();
          }}
        >
          <div className='p-2'>
            <Card className='!rounded-2xl shadow-sm border-0'>
              <div className='mb-2'>
                <Text className='text-lg font-medium'>{t('建议')}</Text>
                <div className='text-xs text-gray-600'>
                  {t(
                    '已根据自动创建设置预填用户名、密码、分组与额度，确认前可修改。',
                  )}
                </div>
              </div>

              <Row gutter={12}>
                <Col span={24}>
                  <Form.Input
                    field='username'
                    label={t('用户名')}
                    rules={[{ required: true, message: t('请输入用户名') }]}
                    showClear
                  />
                </Col>
                <Col span={24}>
                  <Form.Input
                    field='password'
                    label={t('密码')}
                    rules={[{ required: true, message: t('请输入密码') }]}
                    showClear
                  />
                </Col>
                <Col span={24}>
                  <Form.Input
                    field='group'
                    label={t('默认分组')}
                    rules={[{ required: true, message: t('请输入分组') }]}
                    showClear
                  />
                </Col>
                <Col span={24}>
                  <Form.InputNumber
                    field='quota'
                    label={t('默认额度')}
                    min={0}
                    style={{ width: '100%' }}
                  />
                </Col>
              </Row>
            </Card>
          </div>
        </Form>
      </Spin>
    </SideSheet>
  );
};

export default AutoCreateUserModal;
