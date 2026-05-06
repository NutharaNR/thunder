/**
 * Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/* eslint-disable @typescript-eslint/no-unsafe-assignment */
/* eslint-disable @typescript-eslint/no-explicit-any */
/* eslint-disable @typescript-eslint/no-unsafe-member-access */
/* eslint-disable @typescript-eslint/no-unsafe-call */

import {Recovery, useAsgardeo, type EmbeddedFlowComponent} from '@asgardeo/react';
import {FlowComponentRenderer, AuthCardLayout, useDesign} from '@thunderid/design';
import {Box, Button, Alert, AlertTitle, CircularProgress, Typography} from '@wso2/oxygen-ui';
import type {JSX} from 'react';
import {useState} from 'react';
import {Trans, useTranslation} from 'react-i18next';
import {useNavigate} from 'react-router';
import ROUTES from '../../constants/routes';

export default function ForgotPasswordBox(): JSX.Element {
  const navigate = useNavigate();
  const {resolveFlowTemplateLiterals: resolve} = useAsgardeo();
  const {t} = useTranslation();
  const {isDesignEnabled} = useDesign();
  const [flowError, setFlowError] = useState<string | null>(null);

  const signInUrl = ROUTES.AUTH.SIGN_IN;

  return (
    <AuthCardLayout
      variant="ForgotPasswordBox"
      logo={{
        src: {
          light: `${import.meta.env.BASE_URL}/assets/images/logo.svg`,
          dark: `${import.meta.env.BASE_URL}/assets/images/logo-inverted.svg`,
        },
        alt: {light: '', dark: ''},
      }}
      showLogo={!isDesignEnabled}
      logoDisplay={!isDesignEnabled ? {xs: 'flex', md: 'none'} : {display: 'none'}}
    >
      <Recovery
        afterRecoveryUrl={signInUrl}
        onFlowChange={(response: any) => {
          if (response?.failureReason) {
            setFlowError(response.failureReason as string);
          } else {
            setFlowError(null);
          }
        }}
      >
        {({values, fieldErrors, error, touched, handleInputChange, handleSubmit, isLoading, components}: any) => (
          <>
            {!components ? (
              <Box sx={{display: 'flex', justifyContent: 'center', p: 3}}>
                <CircularProgress />
              </Box>
            ) : (
              <>
                {error && (
                  <Alert severity="error" sx={{mb: 2}}>
                    <AlertTitle>{t('recovery:errors.failed.title', 'Recovery failed')}</AlertTitle>
                    {error.message ?? t('recovery:errors.failed.description', 'Something went wrong. Please try again.')}
                  </Alert>
                )}
                {flowError && (
                  <Alert severity="error" sx={{mb: 2}}>
                    {flowError}
                  </Alert>
                )}
                {(components as EmbeddedFlowComponent[]).length > 0 && (
                  <Box sx={{display: 'flex', flexDirection: 'column', gap: 2}}>
                    {(components as EmbeddedFlowComponent[]).map((component, index) => (
                      <FlowComponentRenderer
                        key={component.id ?? index}
                        component={component}
                        index={index}
                        values={values ?? {}}
                        touched={touched}
                        fieldErrors={fieldErrors}
                        isLoading={isLoading}
                        resolve={resolve}
                        onInputChange={handleInputChange}
                        onSubmit={(action, inputs) => {
                          setFlowError(null);
                          void handleSubmit(action, inputs);
                        }}
                      />
                    ))}
                  </Box>
                )}
              </>
            )}

            <Typography sx={{textAlign: 'center', mt: 3}}>
              <Trans i18nKey="recovery:redirect.to.signin">
                Remember your password?
                <Button
                  variant="text"
                  onClick={() => {
                    void navigate(signInUrl);
                  }}
                  sx={{
                    p: 0,
                    minWidth: 'auto',
                    textTransform: 'none',
                    color: 'primary.main',
                    textDecoration: 'underline',
                    '&:hover': {
                      textDecoration: 'underline',
                      backgroundColor: 'transparent',
                    },
                  }}
                >
                  Sign in
                </Button>
              </Trans>
            </Typography>
          </>
        )}
      </Recovery>
    </AuthCardLayout>
  );
}
