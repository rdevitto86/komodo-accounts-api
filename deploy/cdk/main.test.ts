import { describe, it, expect, vi } from 'vitest';
import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import { Template, Match } from 'aws-cdk-lib/assertions';
import type { AccountEnvConfig, ServiceBuildContext } from './main.js';
import {
  STG_CONFIG,
  PROD_CONFIG,
  API_NAME,
  PUBLIC_PORT,
  PRIVATE_PORT,
  VERSION,
  EVAL_RULES_PATH,
  buildStack,
  buildAccountService,
  buildWaf,
  buildAccountAlarms,
  buildAlarmNotifications,
  ALARM_EMAIL,
  buildAccountsTable,
  buildAvatarsBucket,
  createInfra,
} from './main.js';

function makeStack(): [cdk.Stack, cdk.App] {
  const app = new cdk.App();
  return [new cdk.Stack(app, 'TestStack', { env: { account: '123456789012', region: 'us-east-2' } }), app];
}

function makeCtx(stack: cdk.Stack, cfg: AccountEnvConfig): ServiceBuildContext {
  const vpc = ec2.Vpc.fromLookup(stack, 'Vpc', { tags: { Name: cfg.vpcTag } });
  return {
    vpc,
    cluster: new ecs.Cluster(stack, 'Cluster', { vpc }),
    logGroup: new logs.LogGroup(stack, 'LogGroup'),
    cfg,
  };
}

interface PolicyStatement {
  Effect?: string;
  Action?: string | string[];
  Resource?: unknown;
}

function deleteObjectStatements(template: Template, bucketLogicalIdPrefix: string): PolicyStatement[] {
  const statements = Object.values(template.findResources('AWS::IAM::Policy'))
    .flatMap((policy): PolicyStatement[] => policy.Properties?.PolicyDocument?.Statement ?? []);

  return statements.filter((statement) => {
    if (statement.Effect !== 'Allow') return false;
    const actions = ([] as string[]).concat(statement.Action ?? []);
    if (!actions.some((action) => action === 's3:DeleteObject' || action === 's3:DeleteObject*')) return false;
    const resources = ([] as unknown[]).concat(statement.Resource ?? []);
    return resources.some((resource) => JSON.stringify(resource).includes(bucketLogicalIdPrefix));
  });
}

describe('configs', () => {
  it('stg config', () => {
    expect(STG_CONFIG).toMatchObject({
      cpu: 512,
      memory: 1024,
      maxCapacity: 3,
      accountsTable: 'komodo-accounts-stg',
      secretPath: 'komodo/stg/accounts-api',
      vpcTag: 'komodo-stg',
      domainName: 'accounts-stg.komodo.com',
    });
    expect(STG_CONFIG.tags).toMatchObject({ dataClassification: 'pii' });
  });

  it('prod config', () => {
    expect(PROD_CONFIG).toMatchObject({
      cpu: 1024,
      memory: 2048,
      maxCapacity: 6,
      accountsTable: 'komodo-accounts-prod',
      secretPath: 'komodo/prod/accounts-api',
      domainName: 'accounts.komodo.com',
    });
    expect(PROD_CONFIG.tags).toMatchObject({ dataClassification: 'pii' });
  });
});

describe('buildAccountService', () => {
  let stack: cdk.Stack;
  let template: Template;

  beforeAll(() => {
    [stack] = makeStack();
    const ctx = makeCtx(stack, STG_CONFIG);
    buildAccountService(stack, ctx);
    template = Template.fromStack(stack);
  });

  it('creates task def with correct container name and env vars', () => {
    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      ContainerDefinitions: Match.arrayWith([
        Match.objectLike({
          Name: `${API_NAME}-stg`,
          Environment: Match.arrayWith([
            Match.objectLike({ Name: 'APP_NAME', Value: API_NAME }),
            Match.objectLike({ Name: 'PORT', Value: `:${PUBLIC_PORT}` }),
            Match.objectLike({ Name: 'PORT_PRIVATE', Value: `:${PRIVATE_PORT}` }),
            Match.objectLike({ Name: 'VERSION', Value: VERSION }),
            Match.objectLike({ Name: 'EVAL_RULES_PATH', Value: EVAL_RULES_PATH }),
            Match.objectLike({ Name: 'AWS_REGION', Value: 'us-east-2' }),
            Match.objectLike({ Name: 'DYNAMODB_TABLE', Value: 'komodo-accounts-stg' }),
            Match.objectLike({ Name: 'S3_AVATAR_BUCKET', Value: 'komodo-accounts-avatars-stg' }),
          ]),
        }),
      ]),
    });
  });

  it('creates ALB with HTTPS listener and HTTP redirect', () => {
    template.resourceCountIs('AWS::ElasticLoadBalancingV2::LoadBalancer', 1);
    template.hasResourceProperties('AWS::ElasticLoadBalancingV2::Listener', {
      Port: 443,
      Protocol: 'HTTPS',
    });
    template.hasResourceProperties('AWS::ElasticLoadBalancingV2::Listener', {
      Port: 80,
      Protocol: 'HTTP',
      DefaultActions: [Match.objectLike({
        Type: 'redirect',
        RedirectConfig: Match.objectLike({ Protocol: 'HTTPS', StatusCode: 'HTTP_301' }),
      })],
    });
  });

  it('defaults the HTTPS listener to a 404 fixed response for unmatched paths', () => {
    template.hasResourceProperties('AWS::ElasticLoadBalancingV2::Listener', {
      Port: 443,
      DefaultActions: [Match.objectLike({
        Type: 'fixed-response',
        FixedResponseConfig: Match.objectLike({ StatusCode: '404' }),
      })],
    });
  });

  it('creates ALB and service security groups', () => {
    template.resourceCountIs('AWS::EC2::SecurityGroup', 2);
  });

  it('grants secrets manager read access on task role', () => {
    template.hasResourceProperties('AWS::IAM::Policy', {
      PolicyDocument: Match.objectLike({
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith(['secretsmanager:GetSecretValue']),
            Effect: 'Allow',
          }),
        ]),
      }),
    });
  });

  it('configures auto-scaling', () => {
    template.resourceCountIs('AWS::ApplicationAutoScaling::ScalableTarget', 1);
  });

  it('registers Cloud Map service for private port', () => {
    template.resourceCountIs('AWS::ServiceDiscovery::PrivateDnsNamespace', 1);
    template.hasResourceProperties('AWS::ServiceDiscovery::Service', {
      Name: 'accounts-api',
      DnsConfig: Match.objectLike({
        DnsRecords: Match.arrayWith([Match.objectLike({ Type: 'A' })]),
      }),
    });
  });

  it('adds listener rules for each public path', () => {
    template.resourceCountIs('AWS::ElasticLoadBalancingV2::ListenerRule', 5);
  });
});

describe('WAF and Alarms', () => {
  let stack: cdk.Stack;
  let ctx: ServiceBuildContext;
  let svc: ReturnType<typeof buildAccountService>;

  beforeEach(() => {
    [stack] = makeStack();
    ctx = makeCtx(stack, STG_CONFIG);
    svc = buildAccountService(stack, ctx);
  });

  describe('buildWaf', () => {
    it('creates WebACL with managed rules and per-path rate limits', () => {
      buildWaf(stack, svc.alb);
      const template = Template.fromStack(stack);

      template.hasResourceProperties('AWS::WAFv2::WebACL', {
        Scope: 'REGIONAL',
        Rules: Match.arrayWith([
          Match.objectLike({ Name: 'AWSManagedRulesCommonRuleSet' }),
          Match.objectLike({ Name: 'AWSManagedRulesKnownBadInputsRuleSet' }),
          Match.objectLike({ Name: 'ProfileRateLimit' }),
          Match.objectLike({ Name: 'AddressRateLimit' }),
        ]),
      });
      template.resourceCountIs('AWS::WAFv2::WebACLAssociation', 1);
    });
  });

  describe('buildAccountAlarms', () => {
    it('creates metric filters and alarms on top of the FargateService base alarms', () => {
      buildAccountAlarms(stack, ctx.logGroup, svc.alb);
      const template = Template.fromStack(stack);

      template.resourceCountIs('AWS::Logs::MetricFilter', 2);
      template.resourceCountIs('AWS::CloudWatch::Alarm', 7);
    });

    it('dimensions the latency alarm on the load balancer full name, not the ARN', () => {
      buildAccountAlarms(stack, ctx.logGroup, svc.alb);
      const template = Template.fromStack(stack);
      const fullName = stack.resolve(svc.alb.loadBalancerFullName);

      expect(fullName).toEqual({ 'Fn::GetAtt': [expect.any(String), 'LoadBalancerFullName'] });
      template.hasResourceProperties('AWS::CloudWatch::Alarm', {
        MetricName: 'TargetResponseTime',
        Dimensions: [{ Name: 'LoadBalancer', Value: fullName }],
      });
    });
  });

  describe('buildAlarmNotifications', () => {
    it('creates an SNS topic with an email subscription', () => {
      buildAlarmNotifications(stack, STG_CONFIG);
      const template = Template.fromStack(stack);

      template.resourceCountIs('AWS::SNS::Topic', 1);
      template.hasResourceProperties('AWS::SNS::Subscription', {
        Protocol: 'email',
        Endpoint: ALARM_EMAIL,
      });
    });

    it('attaches the topic to every alarm in the stack, leaving none silent', () => {
      buildAccountAlarms(stack, ctx.logGroup, svc.alb);
      const topic = buildAlarmNotifications(stack, STG_CONFIG);
      const template = Template.fromStack(stack);
      const alarms = template.findResources('AWS::CloudWatch::Alarm');
      const expected = [stack.resolve(topic.topicArn)];

      expect(Object.keys(alarms)).toHaveLength(7);
      for (const [logicalId, alarm] of Object.entries(alarms)) {
        expect(alarm.Properties?.AlarmActions, `${logicalId} has no alarm action`).toEqual(expected);
      }
    });
  });
});

describe('buildAccountsTable', () => {
  it('creates DynamoDB table with correct configuration and grants access', () => {
    const [stack] = makeStack();
    const role = new iam.Role(stack, 'TestRole', { assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com') });
    buildAccountsTable(stack, 'dev', 'komodo-accounts-dev', role);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::DynamoDB::Table', {
      BillingMode: 'PAY_PER_REQUEST',
      StreamSpecification: {
        StreamViewType: 'NEW_AND_OLD_IMAGES',
      },
    });

    template.hasResourceProperties('AWS::IAM::Policy', {
      PolicyDocument: Match.objectLike({
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith([
              'dynamodb:PutItem',
              'dynamodb:UpdateItem',
              'dynamodb:DeleteItem',
            ]),
            Effect: 'Allow',
          }),
        ]),
      }),
    });
  });
});

describe('buildAvatarsBucket', () => {
  it('creates bucket with BlockPublicAccess, SSL enforcement, and S3-managed encryption', () => {
    const [stack] = makeStack();
    const role = new iam.Role(stack, 'TestRole', { assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com') });
    buildAvatarsBucket(stack, 'dev', role);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::S3::Bucket', {
      PublicAccessBlockConfiguration: {
        BlockPublicAcls: true,
        BlockPublicPolicy: true,
        IgnorePublicAcls: true,
        RestrictPublicBuckets: true,
      },
      BucketEncryption: {
        ServerSideEncryptionConfiguration: [
          {
            ServerSideEncryptionByDefault: {
              SSEAlgorithm: 'AES256',
            },
          },
        ],
      },
    });

    template.hasResourceProperties('AWS::S3::BucketPolicy', {
      PolicyDocument: Match.objectLike({
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: 's3:*',
            Condition: { Bool: { 'aws:SecureTransport': 'false' } },
            Effect: 'Deny',
          }),
        ]),
      }),
    });
  });

  it('grants the task role object delete, scoped to avatar objects only', () => {
    const [stack] = makeStack();
    const role = new iam.Role(stack, 'TestRole', { assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com') });
    buildAvatarsBucket(stack, 'dev', role);
    const template = Template.fromStack(stack);

    const statements = deleteObjectStatements(template, 'AccountAvatars');

    expect(statements).toHaveLength(1);
    expect(statements[0]?.Resource).toEqual({
      'Fn::Join': ['', [{ 'Fn::GetAtt': [expect.stringMatching(/^AccountAvatars/), 'Arn'] }, '/*']],
    });
  });

  it('sets removalPolicy to RETAIN on prod', () => {
    const [stack] = makeStack();
    const role = new iam.Role(stack, 'TestRole', { assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com') });
    buildAvatarsBucket(stack, 'prod', role);
    const template = Template.fromStack(stack);

    template.hasResource('AWS::S3::Bucket', { DeletionPolicy: 'Retain' });
  });

  it('sets removalPolicy to DESTROY on dev', () => {
    const [stack] = makeStack();
    const role = new iam.Role(stack, 'TestRole', { assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com') });
    buildAvatarsBucket(stack, 'dev', role);
    const template = Template.fromStack(stack);

    template.hasResource('AWS::S3::Bucket', { DeletionPolicy: 'Delete' });
  });
});

describe('buildStack', () => {
  describe('stg config', () => {
    let stack: cdk.Stack;
    let template: Template;

    beforeAll(() => {
      [stack] = makeStack();
      buildStack(stack, STG_CONFIG);
      template = Template.fromStack(stack);
    });

    it('creates stg stack with single service, WAF, alarms, and correct outputs', () => {
      template.resourceCountIs('AWS::ECS::TaskDefinition', 1);
      template.resourceCountIs('AWS::ECS::Service', 1);
      template.resourceCountIs('AWS::S3::Bucket', 2);
      template.hasOutput('AlbDnsName', {});
      template.hasOutput('ClusterName', {});
      template.hasOutput('ServiceName', {});
      template.hasOutput('CloudMapServiceArn', {});
      template.hasOutput('ServiceSecurityGroupId', {});
      template.hasOutput('DomainName', {});
      template.hasOutput('AccountsTableName', {});
      template.hasOutput('AccountsTableStreamArn', {});
      template.hasOutput('AvatarsBucketName', {});
      template.hasOutput('WafWebAclArn', {});
      template.hasOutput('AlarmTopicArn', {});
    });

    it('lets the task role delete an avatar during account closure without widening the grant', () => {
      const avatarDeletes = deleteObjectStatements(template, 'AccountAvatars');
      const resources = ([] as unknown[]).concat(avatarDeletes[0]?.Resource ?? []);
      const serialised = JSON.stringify(resources);

      expect(avatarDeletes).toHaveLength(1);
      expect(resources).toEqual([
        { 'Fn::Join': ['', [{ 'Fn::GetAtt': [expect.stringMatching(/^AccountAvatars/), 'Arn'] }, '/*']] },
      ]);
      expect(serialised).not.toContain('AccountExports');
      expect(resources).not.toContain('*');
    });

    it('applies tags from config', () => {
      const cluster = template.findResources('AWS::ECS::Cluster');
      const clusterTags = Object.values(cluster)[0]?.Properties?.Tags;

      expect(clusterTags).toContainEqual({ Key: 'project', Value: API_NAME });
      expect(clusterTags).toContainEqual({ Key: 'dataClassification', Value: 'pii' });
    });
  });

  it('creates full stack for prod with WAF and alarms', () => {
    const [stack] = makeStack();
    buildStack(stack, PROD_CONFIG);
    const template = Template.fromStack(stack);

    template.resourceCountIs('AWS::ECS::TaskDefinition', 1);
    template.resourceCountIs('AWS::ECS::Service', 1);
    template.resourceCountIs('AWS::S3::Bucket', 2);
    template.hasOutput('AlbDnsName', {});
    template.hasOutput('ClusterName', {});
    template.hasOutput('ServiceName', {});
    template.hasOutput('CloudMapServiceArn', {});
    template.hasOutput('ServiceSecurityGroupId', {});
    template.hasOutput('DomainName', {});
    template.hasOutput('AccountsTableName', {});
    template.hasOutput('AccountsTableStreamArn', {});
    template.hasOutput('AvatarsBucketName', {});
    template.hasOutput('WafWebAclArn', {});
    template.hasOutput('AlarmTopicArn', {});
  });

  it('scales with prod config', () => {
    const [stack] = makeStack();
    buildStack(stack, PROD_CONFIG);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      Cpu: '1024',
      Memory: '2048',
    });
  });
});

describe('createInfra', () => {
  it('exits with error when env context is missing', () => {
    const exitSpy = vi.spyOn(process, 'exit').mockImplementation(() => undefined as never);
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => { });

    createInfra();

    expect(exitSpy).toHaveBeenCalledWith(1);
    expect(errorSpy).toHaveBeenCalledWith('failed to create infrastructure:', expect.any(Error));

    exitSpy.mockRestore();
    errorSpy.mockRestore();
  });

  it('exits with error when env context is invalid (e.g. the retired dev tier)', () => {
    const exitSpy = vi.spyOn(process, 'exit').mockImplementation(() => undefined as never);
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => { });
    const previousContext = process.env.CDK_CONTEXT_JSON;
    process.env.CDK_CONTEXT_JSON = JSON.stringify({ env: 'dev' });

    createInfra();

    expect(exitSpy).toHaveBeenCalledWith(1);
    expect(errorSpy).toHaveBeenCalledWith('failed to create infrastructure:', expect.any(Error));

    if (previousContext === undefined) {
      delete process.env.CDK_CONTEXT_JSON;
    } else {
      process.env.CDK_CONTEXT_JSON = previousContext;
    }
    exitSpy.mockRestore();
    errorSpy.mockRestore();
  });
});
