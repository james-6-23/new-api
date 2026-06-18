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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Button,
  Col,
  Form,
  Input,
  Row,
  Select,
  Space,
  Spin,
  Table,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconArrowUp, IconArrowDown, IconPlus } from '@douyinfe/semi-icons';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const KEYS = {
  username_prefix: 'auto_create_user_setting.username_prefix',
  username_suffix_length: 'auto_create_user_setting.username_suffix_length',
  username_suffix_charset: 'auto_create_user_setting.username_suffix_charset',
  password_mode: 'auto_create_user_setting.password_mode',
  random_password_length: 'auto_create_user_setting.random_password_length',
  default_quota: 'auto_create_user_setting.default_quota',
  default_group: 'auto_create_user_setting.default_group',
  site_url: 'auto_create_user_setting.site_url',
  copy_templates: 'auto_create_user_setting.copy_templates',
};

const DEFAULT_ROWS = [
  { label: '站点', template: '{{site}}' },
  { label: '用户名', template: '{{username}}' },
  { label: '密码', template: '{{password}}' },
];

function safeParseRows(value) {
  if (!value) return DEFAULT_ROWS;
  try {
    const parsed = JSON.parse(value);
    if (!Array.isArray(parsed)) return DEFAULT_ROWS;
    return parsed
      .filter(
        (it) =>
          it && typeof it.label === 'string' && typeof it.template === 'string',
      )
      .map((it) => ({ label: it.label, template: it.template }));
  } catch {
    return DEFAULT_ROWS;
  }
}

export default function SettingsAutoCreateUser(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const refForm = useRef();

  const initial = useMemo(
    () => ({
      [KEYS.username_prefix]: 'User-',
      [KEYS.username_suffix_length]: 4,
      [KEYS.username_suffix_charset]: 'alphanumeric',
      [KEYS.password_mode]: 'same_as_username',
      [KEYS.random_password_length]: 12,
      [KEYS.default_quota]: 0,
      [KEYS.default_group]: 'default',
      [KEYS.site_url]: '',
      [KEYS.copy_templates]: JSON.stringify(DEFAULT_ROWS),
    }),
    [],
  );

  const [inputs, setInputs] = useState(initial);
  const [inputsRow, setInputsRow] = useState(initial);
  const [rows, setRows] = useState(safeParseRows(initial[KEYS.copy_templates]));

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((prev) => ({ ...prev, [fieldName]: value }));
    };
  }

  function commitRows(next) {
    setRows(next);
    const serialized = JSON.stringify(next);
    setInputs((prev) => ({ ...prev, [KEYS.copy_templates]: serialized }));
  }

  useEffect(() => {
    const current = {};
    Object.values(KEYS).forEach((key) => {
      if (key in props.options) {
        current[key] = props.options[key];
      }
    });
    if (Object.keys(current).length === 0) return;
    setInputs((prev) => ({ ...prev, ...current }));
    setInputsRow((prev) => ({ ...prev, ...current }));
    setRows(safeParseRows(current[KEYS.copy_templates]));
    if (refForm.current) {
      refForm.current.setValues({ ...inputs, ...current });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.options]);

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) =>
      API.put('/api/option/', {
        key: item.key,
        value: String(inputs[item.key]),
      }),
    );
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (res.includes(undefined)) {
          return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh?.();
      })
      .catch(() => showError(t('保存失败，请重试')))
      .finally(() => setLoading(false));
  }

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(api) => (refForm.current = api)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('自动创建用户')}>
          <Typography.Text
            type='tertiary'
            style={{ marginBottom: 16, display: 'block' }}
          >
            {t(
              '配置“自动创建”弹窗预填规则与创建后的复制内容模板。占位符：{{username}}、{{password}}、{{site}}',
            )}
          </Typography.Text>

          <Row gutter={16}>
            <Col xs={24} sm={12} md={8}>
              <Form.Input
                field={KEYS.username_prefix}
                label={t('用户名前缀')}
                onChange={handleFieldChange(KEYS.username_prefix)}
                showClear
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.InputNumber
                field={KEYS.username_suffix_length}
                label={t('后缀长度')}
                min={1}
                max={32}
                style={{ width: '100%' }}
                onChange={handleFieldChange(KEYS.username_suffix_length)}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.Select
                field={KEYS.username_suffix_charset}
                label={t('后缀字符集')}
                optionList={[
                  { value: 'alphanumeric', label: t('数字与字母') },
                  { value: 'digits', label: t('仅数字') },
                  { value: 'letters', label: t('仅字母') },
                ]}
                style={{ width: '100%' }}
                onChange={handleFieldChange(KEYS.username_suffix_charset)}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.Select
                field={KEYS.password_mode}
                label={t('密码模式')}
                optionList={[
                  { value: 'same_as_username', label: t('与用户名相同') },
                  { value: 'random', label: t('随机生成') },
                ]}
                style={{ width: '100%' }}
                onChange={handleFieldChange(KEYS.password_mode)}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.InputNumber
                field={KEYS.random_password_length}
                label={t('随机密码长度')}
                min={8}
                max={64}
                style={{ width: '100%' }}
                onChange={handleFieldChange(KEYS.random_password_length)}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.InputNumber
                field={KEYS.default_quota}
                label={t('默认额度')}
                min={0}
                style={{ width: '100%' }}
                onChange={handleFieldChange(KEYS.default_quota)}
              />
            </Col>
            <Col xs={24} sm={12} md={8}>
              <Form.Input
                field={KEYS.default_group}
                label={t('默认分组')}
                onChange={handleFieldChange(KEYS.default_group)}
                showClear
              />
            </Col>
            <Col xs={24} sm={12} md={16}>
              <Form.Input
                field={KEYS.site_url}
                label={t('站点 URL')}
                placeholder='https://example.com'
                onChange={handleFieldChange(KEYS.site_url)}
                showClear
              />
            </Col>
          </Row>

          <Typography.Text strong style={{ marginTop: 16, display: 'block' }}>
            {t('复制模板')}
          </Typography.Text>
          <Typography.Text type='tertiary' size='small'>
            {t('可用占位符：{{username}}、{{password}}、{{site}}')}
          </Typography.Text>
          <Table
            style={{ marginTop: 8 }}
            dataSource={rows.map((r, i) => ({ ...r, _idx: i }))}
            rowKey='_idx'
            pagination={false}
            columns={[
              {
                title: t('名称'),
                dataIndex: 'label',
                width: 160,
                render: (_, row) => (
                  <Input
                    value={row.label}
                    onChange={(v) => {
                      const next = [...rows];
                      next[row._idx] = { ...next[row._idx], label: v };
                      commitRows(next);
                    }}
                  />
                ),
              },
              {
                title: t('模板'),
                dataIndex: 'template',
                render: (_, row) => (
                  <Input
                    value={row.template}
                    onChange={(v) => {
                      const next = [...rows];
                      next[row._idx] = { ...next[row._idx], template: v };
                      commitRows(next);
                    }}
                  />
                ),
              },
              {
                title: t('操作'),
                width: 160,
                render: (_, row) => (
                  <Space>
                    <Button
                      icon={<IconArrowUp />}
                      size='small'
                      disabled={row._idx === 0}
                      onClick={() => {
                        if (row._idx === 0) return;
                        const next = [...rows];
                        const tmp = next[row._idx - 1];
                        next[row._idx - 1] = next[row._idx];
                        next[row._idx] = tmp;
                        commitRows(next);
                      }}
                    />
                    <Button
                      icon={<IconArrowDown />}
                      size='small'
                      disabled={row._idx === rows.length - 1}
                      onClick={() => {
                        if (row._idx === rows.length - 1) return;
                        const next = [...rows];
                        const tmp = next[row._idx + 1];
                        next[row._idx + 1] = next[row._idx];
                        next[row._idx] = tmp;
                        commitRows(next);
                      }}
                    />
                    <Button
                      icon={<IconDelete />}
                      size='small'
                      type='danger'
                      onClick={() => {
                        commitRows(rows.filter((_, i) => i !== row._idx));
                      }}
                    />
                  </Space>
                ),
              },
            ]}
          />
          <Button
            icon={<IconPlus />}
            style={{ marginTop: 8 }}
            onClick={() => commitRows([...rows, { label: '', template: '' }])}
          >
            {t('新增复制项')}
          </Button>

          <Row style={{ marginTop: 16 }}>
            <Col>
              <Button theme='solid' onClick={onSubmit}>
                {t('保存')}
              </Button>
            </Col>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
