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

import React from 'react';
import {
  Banner,
  Button,
  Card,
  Col,
  Form,
  Radio,
  Row,
  TagInput,
  Typography,
} from '@douyinfe/semi-ui';

const { Text } = Typography;

export default function NetworkSettingsCards({ settings }) {
  const {
    allowedPorts,
    domainFilterMode,
    domainList,
    handleCheckboxChange,
    ipFilterMode,
    ipList,
    setAllowedPorts,
    setDomainFilterMode,
    setDomainList,
    setInputs,
    setIpFilterMode,
    setIpList,
    submitSSRF,
    submitServerAddress,
    submitWorker,
    t,
  } = settings;

  return (
    <>
      <Card>
        <Form.Section text={t('通用设置')}>
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={24} lg={24} xl={24}>
              <Form.Input
                field='ServerAddress'
                label={t('服务器地址')}
                placeholder='https://yourdomain.com'
                extraText={t(
                  '该服务器地址将影响支付回调地址以及默认首页展示的地址，请确保正确配置',
                )}
              />
            </Col>
          </Row>
          <Button onClick={submitServerAddress}>{t('更新服务器地址')}</Button>
        </Form.Section>
      </Card>

      <Card>
        <Form.Section text={t('代理设置')}>
          <Banner
            type='info'
            description={t(
              '此代理仅用于图片请求转发，Webhook通知发送等，AI API请求仍然由服务器直接发出，可在渠道设置中单独配置代理',
            )}
            style={{ marginBottom: 20, marginTop: 16 }}
          />
          <Text>
            {t('仅支持')}{' '}
            <a
              href='https://github.com/Calcium-Ion/new-api-worker'
              target='_blank'
              rel='noopener noreferrer'
            >
              new-api-worker
            </a>{' '}
            {t('或其兼容new-api-worker格式的其他版本')}
          </Text>
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='WorkerUrl'
                label={t('Worker地址')}
                placeholder='例如：https://workername.yourdomain.workers.dev'
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='WorkerValidKey'
                label={t('Worker密钥')}
                placeholder='敏感信息不会发送到前端显示'
                type='password'
              />
            </Col>
          </Row>
          <Form.Checkbox field='WorkerAllowHttpImageRequestEnabled' noLabel>
            {t('允许 HTTP 协议图片请求（适用于自部署代理）')}
          </Form.Checkbox>
          <Button onClick={submitWorker}>{t('更新Worker设置')}</Button>
        </Form.Section>
      </Card>

      <Card>
        <Form.Section text={t('SSRF防护设置')}>
          <Text extraText={t('SSRF防护详细说明')}>
            {t('配置服务器端请求伪造(SSRF)防护，用于保护内网资源安全')}
          </Text>
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={24} lg={24} xl={24}>
              <Form.Checkbox
                field='fetch_setting.enable_ssrf_protection'
                noLabel
                extraText={t('SSRF防护开关详细说明')}
                onChange={(e) =>
                  handleCheckboxChange(
                    'fetch_setting.enable_ssrf_protection',
                    e,
                  )
                }
              >
                {t('启用SSRF防护（推荐开启以保护服务器安全）')}
              </Form.Checkbox>
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={24} lg={24} xl={24}>
              <Form.Checkbox
                field='fetch_setting.allow_private_ip'
                noLabel
                extraText={t('私有IP访问详细说明')}
                onChange={(e) =>
                  handleCheckboxChange('fetch_setting.allow_private_ip', e)
                }
              >
                {t('允许访问私有IP地址（127.0.0.1、192.168.x.x等内网地址）')}
              </Form.Checkbox>
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={24} lg={24} xl={24}>
              <Form.Checkbox
                field='fetch_setting.apply_ip_filter_for_domain'
                noLabel
                extraText={t('域名IP过滤详细说明')}
                onChange={(e) =>
                  handleCheckboxChange(
                    'fetch_setting.apply_ip_filter_for_domain',
                    e,
                  )
                }
                style={{ marginBottom: 8 }}
              >
                {t('对域名启用 IP 过滤（推荐开启）')}
              </Form.Checkbox>
              <Text strong>
                {t(domainFilterMode ? '域名白名单' : '域名黑名单')}
              </Text>
              <Text
                type='secondary'
                style={{ display: 'block', marginBottom: 8 }}
              >
                {t('支持通配符格式，如：example.com, *.api.example.com')}
              </Text>
              <Radio.Group
                type='button'
                value={domainFilterMode ? 'whitelist' : 'blacklist'}
                onChange={(val) => {
                  const selected = val && val.target ? val.target.value : val;
                  const isWhitelist = selected === 'whitelist';
                  setDomainFilterMode(isWhitelist);
                  setInputs((prev) => ({
                    ...prev,
                    'fetch_setting.domain_filter_mode': isWhitelist,
                  }));
                }}
                style={{ marginBottom: 8 }}
              >
                <Radio value='whitelist'>{t('白名单')}</Radio>
                <Radio value='blacklist'>{t('黑名单')}</Radio>
              </Radio.Group>
              <TagInput
                value={domainList}
                onChange={(value) => {
                  setDomainList(value);
                  // 触发Form的onChange事件
                  setInputs((prev) => ({
                    ...prev,
                    'fetch_setting.domain_list': value,
                  }));
                }}
                placeholder={t('输入域名后回车，如：example.com')}
                style={{ width: '100%' }}
              />
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={24} lg={24} xl={24}>
              <Text strong>{t(ipFilterMode ? 'IP白名单' : 'IP黑名单')}</Text>
              <Text
                type='secondary'
                style={{ display: 'block', marginBottom: 8 }}
              >
                {t('支持CIDR格式，如：8.8.8.8, 192.168.1.0/24')}
              </Text>
              <Radio.Group
                type='button'
                value={ipFilterMode ? 'whitelist' : 'blacklist'}
                onChange={(val) => {
                  const selected = val && val.target ? val.target.value : val;
                  const isWhitelist = selected === 'whitelist';
                  setIpFilterMode(isWhitelist);
                  setInputs((prev) => ({
                    ...prev,
                    'fetch_setting.ip_filter_mode': isWhitelist,
                  }));
                }}
                style={{ marginBottom: 8 }}
              >
                <Radio value='whitelist'>{t('白名单')}</Radio>
                <Radio value='blacklist'>{t('黑名单')}</Radio>
              </Radio.Group>
              <TagInput
                value={ipList}
                onChange={(value) => {
                  setIpList(value);
                  // 触发Form的onChange事件
                  setInputs((prev) => ({
                    ...prev,
                    'fetch_setting.ip_list': value,
                  }));
                }}
                placeholder={t('输入IP地址后回车，如：8.8.8.8')}
                style={{ width: '100%' }}
              />
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={24} lg={24} xl={24}>
              <Text strong>{t('允许的端口')}</Text>
              <Text
                type='secondary'
                style={{ display: 'block', marginBottom: 8 }}
              >
                {t('支持单个端口和端口范围，如：80, 443, 8000-8999')}
              </Text>
              <TagInput
                value={allowedPorts}
                onChange={(value) => {
                  setAllowedPorts(value);
                  // 触发Form的onChange事件
                  setInputs((prev) => ({
                    ...prev,
                    'fetch_setting.allowed_ports': value,
                  }));
                }}
                placeholder={t('输入端口后回车，如：80 或 8000-8999')}
                style={{ width: '100%' }}
              />
              <Text
                type='secondary'
                style={{ display: 'block', marginBottom: 8 }}
              >
                {t('端口配置详细说明')}
              </Text>
            </Col>
          </Row>

          <Button onClick={submitSSRF} style={{ marginTop: 16 }}>
            {t('更新SSRF防护设置')}
          </Button>
        </Form.Section>
      </Card>
    </>
  );
}
